package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ubuntu/authd"
	"github.com/ubuntu/authd/internal/log"
)

func sendEvent(msg tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return msg
	}
}

// startBrokerSession returns the sessionID after marking a broker as current.
func startBrokerSession(client authd.PAMClient, brokerID, username string) tea.Cmd {
	return func() tea.Msg {
		if brokerID == "local" {
			return pamIgnore{}
		}

		// Start a transaction for this user with the broker.
		// TODO: gdm case?
		lang := "C"
		for _, e := range []string{"LANG", "LC_MESSAGES", "LC_ALL"} {
			l := os.Getenv(e)
			if l != "" {
				lang = l
			}
		}
		lang = strings.TrimSuffix(lang, ".UTF-8")

		sbReq := &authd.SBRequest{
			BrokerId: brokerID,
			Username: username,
			Lang:     lang,
		}

		sbResp, err := client.SelectBroker(context.TODO(), sbReq)
		if err != nil {
			return pamSystemError{msg: fmt.Sprintf("can't select broker: %v", err)}
		}

		sessionID := sbResp.GetSessionId()
		if sessionID == "" {
			return pamSystemError{msg: "no session ID returned by broker"}
		}
		encryptionKey := sbResp.GetEncryptionKey()
		if encryptionKey == "" {
			return pamSystemError{msg: "no encryption key returned by broker"}
		}

		return SessionStarted{
			sessionID:     sessionID,
			encryptionKey: encryptionKey,
		}
	}
}

func getLayout(client authd.PAMClient, sessionID, authModeID string) tea.Cmd {
	return func() tea.Msg {
		samReq := &authd.SAMRequest{
			SessionId:            sessionID,
			AuthenticationModeId: authModeID,
		}
		uiInfo, err := client.SelectAuthenticationMode(context.TODO(), samReq)
		if err != nil {
			return pamSystemError{
				// TODO: probably go back to broker selection here
				msg: fmt.Sprintf("can't select authentication mode: %v", err),
			}
		}

		if uiInfo.UiLayoutInfo == nil {
			return pamSystemError{
				// TODO: probably go back to broker selection here
				msg: "invalid empty UI Layout information from broker",
			}
		}

		return UILayoutReceived{
			layout: uiInfo.GetUiLayoutInfo(),
		}
	}
}

func (m *model) quit() tea.Cmd {
	if m.currentSession == nil {
		return tea.Quit
	}
	return tea.Sequence(endSession(m.client, m.currentSession), tea.Quit)
}

func endSession(client authd.PAMClient, currentSession *sessionInfo) tea.Cmd {
	if currentSession == nil {
		return nil
	}
	return func() tea.Msg {
		_, err := client.EndSession(context.Background(), &authd.ESRequest{
			SessionId: currentSession.sessionID,
		})
		if err != nil {
			log.Infof(context.Background(), "Could not end session %q. Considering already done", currentSession.sessionID)
			return nil
		}
		return SessionEnded{}
	}
}
