package whatsmeow

import (
	"bytes"
	"context"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/nocodeleaks/whatsfuck/appstate"
	waBinary "github.com/nocodeleaks/whatsfuck/binary"
	"github.com/nocodeleaks/whatsfuck/proto/waE2E"
	"github.com/nocodeleaks/whatsfuck/proto/waServerSync"
	"github.com/nocodeleaks/whatsfuck/proto/waSyncAction"
	"github.com/nocodeleaks/whatsfuck/store"
	"github.com/nocodeleaks/whatsfuck/types"
	waLog "github.com/nocodeleaks/whatsfuck/util/log"
)

func TestGroupMessagePreservesExplicitParticipantAlias(t *testing.T) {
	own := types.NewJID("15550000000", types.DefaultUserServer)
	client := NewClient(&store.Device{ID: &own}, waLog.Noop)
	phone := types.NewJID("15550000001", types.DefaultUserServer)
	lid := types.NewJID("123456", types.HiddenUserServer)
	for _, sender := range []types.JID{lid, phone} {
		for _, addressingMode := range []string{"", "pn", "lid"} {
			t.Run(sender.Server+"/"+addressingMode, func(t *testing.T) {
				source, err := client.parseMessageSource(&waBinary.Node{Attrs: waBinary.Attrs{
					"from": types.NewJID("1234-5678", types.GroupServer), "participant": sender,
					"participant_pn": phone, "participant_lid": lid, "addressing_mode": addressingMode,
				}}, true)
				want := phone
				if sender == phone {
					want = lid
				}
				if err != nil || source.SenderAlt != want {
					t.Fatalf("participant alias = %s, want %s, error = %v", source.SenderAlt, want, err)
				}
			})
		}
	}
}

type upstreamMessageSecrets struct {
	store.NoopStore
	entries []store.MessageSecretInsert
}

func (s *upstreamMessageSecrets) PutMessageSecrets(_ context.Context, entries []store.MessageSecretInsert) error {
	s.entries = append(s.entries, entries...)
	return nil
}

func (s *upstreamMessageSecrets) PutMessageSecret(ctx context.Context, chat, sender types.JID, id types.MessageID, secret []byte) error {
	return s.PutMessageSecrets(ctx, []store.MessageSecretInsert{{Chat: chat, Sender: sender, ID: id, Secret: secret}})
}

func TestWASARootSecretAppStatePersistsForBotAndOwnLID(t *testing.T) {
	secrets := &upstreamMessageSecrets{}
	own := types.NewJID("123456", types.HiddenUserServer)
	bot := types.NewJID("1807055946647697", types.BotServer)
	client := NewClient(&store.Device{LID: own, MsgSecrets: secrets}, waLog.Noop)
	client.dispatchAppState(context.Background(), appstate.WAPatchRegular, appstate.Mutation{
		Operation: waServerSync.SyncdMutation_SET,
		Index:     []string{appstate.IndexWasaRootSecretAction, bot.String()},
		Action: &waSyncAction.SyncActionValue{WasaRootSecretAction: &waSyncAction.WASARootSecretAction{
			Secrets: []*waSyncAction.WASARootSecretAction_RootSecretEntry{{ID: proto.String("secret-id"), RootSecret: []byte("secret-value")}},
		}},
	}, false)
	if len(secrets.entries) != 1 {
		t.Fatalf("stored %d secrets", len(secrets.entries))
	}
	got := secrets.entries[0]
	if got.Chat != bot || got.Sender != own || got.ID != "secret-id" || !bytes.Equal(got.Secret, []byte("secret-value")) {
		t.Fatalf("incorrect secret association: %#v", got)
	}
}

func TestRootSecretDistributionOnlyRedirectsOwnMessages(t *testing.T) {
	for _, ownMessage := range []bool{false, true} {
		for _, wrapped := range []bool{false, true} {
			secrets := &upstreamMessageSecrets{}
			client := NewClient(&store.Device{MsgSecrets: secrets}, waLog.Noop)
			original := types.NewJID("11111", types.HiddenUserServer)
			target := types.NewJID("22222", types.BotServer)
			info := &types.MessageInfo{MessageSource: types.MessageSource{Chat: original, Sender: original, IsFromMe: ownMessage}, ID: "message-id"}
			message := &waE2E.Message{RootSecretDistributeMessage: &waE2E.RootSecretDistributeMessage{ChatJID: proto.String(target.String())}}
			if wrapped {
				message = &waE2E.Message{DeviceSentMessage: &waE2E.DeviceSentMessage{Message: message}}
			}
			message.MessageContextInfo = &waE2E.MessageContextInfo{MessageSecret: []byte("secret")}
			client.storeMessageSecret(context.Background(), info, message)
			want := original
			if ownMessage {
				want = target
			}
			if len(secrets.entries) != 1 || secrets.entries[0].Chat != want {
				t.Fatalf("own=%v wrapped=%v: incorrect stored destination: %#v", ownMessage, wrapped, secrets.entries)
			}
		}
	}
}
