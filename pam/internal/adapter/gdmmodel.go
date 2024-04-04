package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/msteinert/pam/v2"
	"github.com/ubuntu/authd"
	"github.com/ubuntu/authd/internal/brokers"
	"github.com/ubuntu/authd/internal/log"
	"github.com/ubuntu/authd/pam/internal/gdm"
	"github.com/ubuntu/authd/pam/internal/proto"
)

const (
	gdmPollFrequency time.Duration = time.Millisecond * 16
)

type gdmModel struct {
	pamMTx pam.ModuleTransaction

	waitingAuth bool

	// Given the bubbletea async nature we may end up receiving and forwarding
	// events after we've got a PamReturnStatus and even after the PAM module
	// has returned to libpam caller (since go goroutines can still be alive).
	// However, after the quit point we should really not interact anymore with
	// GDM or we'll make it crash (as it doesn't expect any conversation
	// happening at that point).
	// So we ue this as a control point, once we've set this to true, no further
	// conversation with GDM should happen.
	conversationsStopped bool
}

type gdmPollDone struct{}

// Init initializes the main model orchestrator.
func (m *gdmModel) Init() tea.Cmd {
	return tea.Sequence(m.protoHello(),
		requestUICapabilities(m.pamMTx),
		m.pollGdm())
}

func (m *gdmModel) protoHello() tea.Cmd {
	reply, err := gdm.SendData(m.pamMTx, &gdm.Data{Type: gdm.DataType_hello})
	if err != nil {
		return sendEvent(pamError{
			status: pam.ErrCredUnavail,
			msg:    fmt.Sprintf("GDM initialization failed: %v", err),
		})
	}
	if reply.Type != gdm.DataType_hello || reply.Hello == nil ||
		reply.Hello.Version != gdm.ProtoVersion {
		version := -1
		if reply.Hello != nil {
			version = int(reply.Hello.Version)
		}
		return sendEvent(pamError{
			status: pam.ErrCredUnavail,
			msg: fmt.Sprintf("GDM protocol initialization failed, type %s, version %d",
				reply.Type, version),
		})
	}
	log.Debugf(context.TODO(), "Gdm Reply is %v", reply)
	return nil
}

func requestUICapabilities(mTx pam.ModuleTransaction) tea.Cmd {
	return func() tea.Msg {
		res, err := gdm.SendRequestTyped[*gdm.ResponseData_UiLayoutCapabilities](mTx,
			&gdm.RequestData_UiLayoutCapabilities{})
		if err != nil {
			return pamError{
				status: pam.ErrSystem,
				msg:    fmt.Sprintf("Sending GDM UI capabilities Request failed: %v", err),
			}
		}
		if res == nil {
			return supportedUILayoutsReceived{}
		}
		return supportedUILayoutsReceived{res.UiLayoutCapabilities.SupportedUiLayouts}
	}
}

func (m *gdmModel) pollGdm() tea.Cmd {
	gdmPollResults, err := gdm.SendPoll(m.pamMTx)
	if err != nil {
		return sendEvent(pamError{
			status: pam.ErrSystem,
			msg:    fmt.Sprintf("Sending GDM poll failed: %v", err),
		})
	}
	log.Debugf(context.TODO(), "Gdm Poll response is %v", gdmPollResults)

	commands := []tea.Cmd{sendEvent(gdmPollDone{})}

	for _, result := range gdmPollResults {
		switch res := result.Data.(type) {
		case *gdm.EventData_UserSelected:
			commands = append(commands, sendEvent(userSelected{
				username: res.UserSelected.UserId,
			}))

		case *gdm.EventData_BrokerSelected:
			if res.BrokerSelected == nil {
				return sendEvent(pamError{status: pam.ErrSystem,
					msg: "missing broker selected",
				})
			}
			commands = append(commands, sendEvent(brokerSelected{
				brokerID: res.BrokerSelected.BrokerId,
			}))

		case *gdm.EventData_AuthModeSelected:
			if res.AuthModeSelected == nil {
				return sendEvent(pamError{
					status: pam.ErrSystem, msg: "missing auth mode id",
				})
			}
			commands = append(commands, selectAuthMode(res.AuthModeSelected.AuthModeId))

		case *gdm.EventData_IsAuthenticatedRequested:
			if !m.waitingAuth {
				log.Warningf(context.TODO(), "unexpected authentication received: %#v", res.IsAuthenticatedRequested)
				break
			}
			m.waitingAuth = false
			if res.IsAuthenticatedRequested == nil || res.IsAuthenticatedRequested.AuthenticationData == nil {
				return sendEvent(pamError{
					status: pam.ErrSystem, msg: "missing auth requested",
				})
			}
			commands = append(commands, sendEvent(isAuthenticatedRequested{
				item: res.IsAuthenticatedRequested.GetAuthenticationData().Item,
			}))

		case *gdm.EventData_ReselectAuthMode:
			commands = append(commands, sendEvent(reselectAuthMode{}))

		case *gdm.EventData_IsAuthenticatedCancelled:
			if m.waitingAuth {
				commands = append(commands, sendEvent(isAuthenticatedCancelled{}))
			}

		case *gdm.EventData_StageChanged:
			if res.StageChanged == nil {
				return sendEvent(pamError{
					status: pam.ErrSystem, msg: "missing stage changed",
				})
			}
			log.Infof(context.TODO(), "GDM Stage changed to %s", res.StageChanged.Stage)

			if m.waitingAuth && res.StageChanged.Stage != proto.Stage_challenge {
				// Maybe this can be sent only if we ever hit the challenge phase.
				commands = append(commands, sendEvent(isAuthenticatedCancelled{}))
			}
			commands = append(commands, sendEvent(ChangeStage{res.StageChanged.Stage}))
		}
	}
	return tea.Batch(commands...)
}

func (m *gdmModel) emitEvent(event gdm.Event) tea.Cmd {
	return func() tea.Msg {
		return m.emitEventSync(event)
	}
}

func (m *gdmModel) emitEventSync(event gdm.Event) tea.Msg {
	err := gdm.EmitEvent(m.pamMTx, event)
	log.Debug(context.TODO(), "EventSend", event, "result", err)
	if err != nil {
		return pamError{
			status: pam.ErrSystem,
			msg:    fmt.Sprintf("Sending GDM event failed: %v", err),
		}
	}
	return nil
}

func (m gdmModel) Update(msg tea.Msg) (gdmModel, tea.Cmd) {
	if m.conversationsStopped {
		return m, nil
	}

	switch msg := msg.(type) {
	case gdmPollDone:
		return m, tea.Sequence(
			tea.Tick(gdmPollFrequency, func(time.Time) tea.Msg { return nil }),
			m.pollGdm())

	case userSelected:
		return m, m.emitEvent(&gdm.EventData_UserSelected{
			UserSelected: &gdm.Events_UserSelected{UserId: msg.username},
		})

	case brokersListReceived:
		return m, m.emitEvent(&gdm.EventData_BrokersReceived{
			BrokersReceived: &gdm.Events_BrokersReceived{BrokersInfos: msg.brokers},
		})

	case brokerSelected:
		return m, m.emitEvent(&gdm.EventData_BrokerSelected{
			BrokerSelected: &gdm.Events_BrokerSelected{BrokerId: msg.brokerID},
		})

	case authModesReceived:
		return m, m.emitEvent(&gdm.EventData_AuthModesReceived{
			AuthModesReceived: &gdm.Events_AuthModesReceived{AuthModes: msg.authModes},
		})

	case authModeSelected:
		return m, m.emitEvent(&gdm.EventData_AuthModeSelected{
			AuthModeSelected: &gdm.Events_AuthModeSelected{AuthModeId: msg.id},
		})

	case UILayoutReceived:
		return m, sendEvent(m.emitEventSync(&gdm.EventData_UiLayoutReceived{
			UiLayoutReceived: &gdm.Events_UiLayoutReceived{UiLayout: msg.layout},
		}))

	case startAuthentication:
		if m.waitingAuth {
			log.Warning(context.TODO(), "Ignored authentication start request while one is still going")
			return m, nil
		}
		m.waitingAuth = true
		return m, sendEvent(m.emitEventSync(&gdm.EventData_StartAuthentication{
			StartAuthentication: &gdm.Events_StartAuthentication{},
		}))

	case isAuthenticatedResultReceived:
		access := msg.access
		authMsg, err := dataToMsg(msg.msg)
		if err != nil {
			return m, sendEvent(pamError{status: pam.ErrSystem, msg: err.Error()})
		}

		switch access {
		case brokers.AuthGranted:
		case brokers.AuthDenied:
		case brokers.AuthCancelled:
			return m, sendEvent(isAuthenticatedCancelled{})
		case brokers.AuthRetry:
		case brokers.AuthNext:
		default:
			accessJSON, _ := json.Marshal(fmt.Sprintf("Access %q is not valid", access))
			return m, sendEvent(isAuthenticatedResultReceived{
				access: brokers.AuthDenied,
				msg:    fmt.Sprintf(`{"message": %s}`, accessJSON),
			})
		}

		return m, sendEvent(m.emitEventSync(&gdm.EventData_AuthEvent{
			AuthEvent: &gdm.Events_AuthEvent{Response: &authd.IAResponse{
				Access: access,
				Msg:    authMsg,
			}},
		}))

	case isAuthenticatedCancelled:
		m.waitingAuth = false

		return m, sendEvent(m.emitEventSync(&gdm.EventData_AuthEvent{
			AuthEvent: &gdm.Events_AuthEvent{Response: &authd.IAResponse{
				Access: brokers.AuthCancelled,
				Msg:    msg.msg,
			}},
		}))
	}

	return m, nil
}

func (m gdmModel) changeStage(s proto.Stage) tea.Cmd {
	if m.conversationsStopped {
		return nil
	}

	return func() tea.Msg {
		_, err := gdm.SendRequest(m.pamMTx, &gdm.RequestData_ChangeStage{
			ChangeStage: &gdm.Requests_ChangeStage{Stage: s},
		})
		if err != nil {
			return pamError{
				status: pam.ErrSystem,
				msg:    fmt.Sprintf("Changing GDM stage failed: %v", err),
			}
		}
		log.Debugf(context.TODO(), "Gdm stage change to %v sent", s)
		return nil
	}
}

func (m gdmModel) stopConversations() gdmModel {
	// We're about to exit: let's ensure that all the messages have been processed.

	wait := make(chan struct{})
	go func() {
		for {
			time.Sleep(gdmPollFrequency + 1)
			if !gdm.ConversationInProgress() {
				break
			}
		}
		wait <- struct{}{}
	}()

	select {
	case <-wait:
	case <-time.After(time.Second):
		log.Error(context.TODO(), "Failed waiting for GDM tasks completion")
	}

	m.conversationsStopped = true
	return m
}
