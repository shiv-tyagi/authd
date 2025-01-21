package tempentries

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/ubuntu/authd/internal/testutils/golden"
	"github.com/ubuntu/authd/internal/users/idgenerator"
	"github.com/ubuntu/authd/internal/users/types"
)

func TestRegisterGroup(t *testing.T) {
	t.Parallel()

	gidToGenerate := uint32(12345)

	tests := map[string]struct {
		groupName      string
		gidsToGenerate []uint32

		wantErr bool
	}{
		"Successfully_register_a_new_group": {},
		"Successfully_register_a_group_if_the_first_generated_GID_is_already_in_use": {
			gidsToGenerate: []uint32{0, gidToGenerate}, // GID 0 (root) always exists
		},

		"Error_when_name_is_already_in_use": {groupName: "root", wantErr: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if tc.groupName == "" {
				tc.groupName = "authd-temp-groups-test"
			}

			if tc.gidsToGenerate == nil {
				tc.gidsToGenerate = []uint32{gidToGenerate}
			}

			idGeneratorMock := &idgenerator.IDGeneratorMock{GIDsToGenerate: tc.gidsToGenerate}
			records := newTemporaryGroupRecords(idGeneratorMock)

			gid, cleanup, err := records.RegisterGroup(tc.groupName)
			if tc.wantErr {
				require.Error(t, err, "RegisterGroup should return an error, but did not")
				return
			}
			require.NoError(t, err, "RegisterGroup should not return an error, but did")
			require.Equal(t, gidToGenerate, gid, "GID should be the one generated by the IDGenerator")
			// Check that the temporary group was created
			group, err := records.GroupByID(gid)
			require.NoError(t, err, "GroupByID should not return an error, but did")
			checkGroup(t, group)

			// Delete the temporary group
			cleanup()

			// Check that the temporary group was deleted
			_, err = records.GroupByID(gid)
			require.Error(t, err, "GroupByID should return an error, but did not")
		})
	}
}

func TestGroupByIDAndName(t *testing.T) {
	t.Parallel()

	groupName := "authd-temp-groups-test"
	gidToGenerate := uint32(12345)

	tests := map[string]struct {
		registerGroup       bool
		groupAlreadyRemoved bool
		byName              bool

		wantErr bool
	}{
		"Successfully_get_a_group_by_ID":   {registerGroup: true},
		"Successfully_get_a_group_by_name": {registerGroup: true, byName: true},

		"Error_when_group_is_not_registered_-_GroupByID":   {wantErr: true},
		"Error_when_group_is_not_registered_-_GroupByName": {byName: true, wantErr: true},
		"Error_when_group_is_already_removed_-_GroupByID": {
			registerGroup:       true,
			groupAlreadyRemoved: true,
			wantErr:             true,
		},
		"Error_when_group_is_already_removed_-_GroupByName": {
			registerGroup:       true,
			groupAlreadyRemoved: true,
			byName:              true,
			wantErr:             true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			idGeneratorMock := &idgenerator.IDGeneratorMock{GIDsToGenerate: []uint32{gidToGenerate}}
			records := newTemporaryGroupRecords(idGeneratorMock)

			if tc.registerGroup {
				gid, cleanup, err := records.RegisterGroup(groupName)
				require.NoError(t, err, "RegisterGroup should not return an error, but did")
				require.Equal(t, gidToGenerate, gid, "GID should be the one generated by the IDGenerator")

				if tc.groupAlreadyRemoved {
					cleanup()
				} else {
					defer cleanup()
				}
			}

			var group types.GroupEntry
			var err error
			if tc.byName {
				group, err = records.GroupByID(gidToGenerate)
			} else {
				group, err = records.GroupByName(groupName)
			}

			if tc.wantErr {
				require.Error(t, err, "GroupByID should return an error, but did not")
				return
			}
			require.NoError(t, err, "GroupByID should not return an error, but did")
			checkGroup(t, group)
		})
	}
}

func checkGroup(t *testing.T, group types.GroupEntry) {
	t.Helper()

	// The passwd field is randomly generated, so unset it before comparing the group with the golden file.
	require.NotEmpty(t, group.Passwd, "Passwd should not be empty")
	group.Passwd = ""

	golden.CheckOrUpdateYAML(t, group)
}
