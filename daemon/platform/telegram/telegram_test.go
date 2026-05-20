package telegram

import (
	"testing"
	"time"

	"gopkg.in/telebot.v3"

	"asika/common/platforms"
	commonutil "asika/common/platformutil"
)

func TestBotCreation(t *testing.T) {
	bot, cleanup := setupBotTest(t)
	defer cleanup()
	if bot == nil {
		t.Fatal("bot should not be nil")
	}
	if len(bot.adminIDs) != 2 {
		t.Errorf("expected 2 admin IDs, got %d", len(bot.adminIDs))
	}
}

type fakeCtx struct {
	sender *telebot.User
}

func (f *fakeCtx) Bot() *telebot.Bot                                 { return nil }
func (f *fakeCtx) Update() telebot.Update                            { return telebot.Update{} }
func (f *fakeCtx) Message() *telebot.Message                         { return nil }
func (f *fakeCtx) Callback() *telebot.Callback                       { return nil }
func (f *fakeCtx) Query() *telebot.Query                             { return nil }
func (f *fakeCtx) InlineResult() *telebot.InlineResult               { return nil }
func (f *fakeCtx) ShippingQuery() *telebot.ShippingQuery             { return nil }
func (f *fakeCtx) PreCheckoutQuery() *telebot.PreCheckoutQuery       { return nil }
func (f *fakeCtx) Poll() *telebot.Poll                               { return nil }
func (f *fakeCtx) PollAnswer() *telebot.PollAnswer                   { return nil }
func (f *fakeCtx) ChatMember() *telebot.ChatMemberUpdate             { return nil }
func (f *fakeCtx) ChatJoinRequest() *telebot.ChatJoinRequest         { return nil }
func (f *fakeCtx) Migration() (int64, int64)                         { return 0, 0 }
func (f *fakeCtx) Topic() *telebot.Topic                             { return nil }
func (f *fakeCtx) Boost() *telebot.BoostUpdated                      { return nil }
func (f *fakeCtx) BoostRemoved() *telebot.BoostRemoved               { return nil }
func (f *fakeCtx) Sender() *telebot.User                             { return f.sender }
func (f *fakeCtx) Chat() *telebot.Chat                               { return nil }
func (f *fakeCtx) Recipient() telebot.Recipient                      { return f.sender }
func (f *fakeCtx) Text() string                                      { return "" }
func (f *fakeCtx) Entities() telebot.Entities                        { return nil }
func (f *fakeCtx) Data() string                                      { return "" }
func (f *fakeCtx) Args() []string                                    { return nil }
func (f *fakeCtx) Send(interface{}, ...interface{}) error            { return nil }
func (f *fakeCtx) SendAlbum(telebot.Album, ...interface{}) error     { return nil }
func (f *fakeCtx) Reply(interface{}, ...interface{}) error           { return nil }
func (f *fakeCtx) Forward(telebot.Editable, ...interface{}) error    { return nil }
func (f *fakeCtx) ForwardTo(telebot.Recipient, ...interface{}) error { return nil }
func (f *fakeCtx) Edit(interface{}, ...interface{}) error            { return nil }
func (f *fakeCtx) EditCaption(string, ...interface{}) error          { return nil }
func (f *fakeCtx) EditOrSend(interface{}, ...interface{}) error      { return nil }
func (f *fakeCtx) EditOrReply(interface{}, ...interface{}) error     { return nil }
func (f *fakeCtx) Delete() error                                     { return nil }
func (f *fakeCtx) DeleteAfter(time.Duration) *time.Timer             { return nil }
func (f *fakeCtx) Notify(telebot.ChatAction) error                   { return nil }
func (f *fakeCtx) Ship(...interface{}) error                         { return nil }
func (f *fakeCtx) Accept(...string) error                            { return nil }
func (f *fakeCtx) Answer(*telebot.QueryResponse) error               { return nil }
func (f *fakeCtx) Respond(...*telebot.CallbackResponse) error        { return nil }
func (f *fakeCtx) RespondText(string) error                          { return nil }
func (f *fakeCtx) RespondAlert(string) error                         { return nil }
func (f *fakeCtx) Archive() error                                    { return nil }
func (f *fakeCtx) Pin() error                                        { return nil }
func (f *fakeCtx) Unpin() error                                      { return nil }
func (f *fakeCtx) SetReaction(...telebot.Reaction) error             { return nil }
func (f *fakeCtx) Get(string) interface{}                            { return nil }
func (f *fakeCtx) Set(string, interface{})                           {}

func TestIsAdmin_EmptyAdminIDs(t *testing.T) {
	bot, cleanup := setupBotTest(t)
	defer cleanup()
	bot.adminIDs = map[int64]bool{}
	bot.operatorIDs = map[int64]bool{}
	bot.viewerIDs = map[int64]bool{}
	ctx := &fakeCtx{sender: &telebot.User{ID: 12345}}
	if bot.isAdmin(ctx) {
		t.Error("with empty allowlists, no one should be admin")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"", 10, ""},
	}
	for _, tt := range tests {
		got := commonutil.Truncate(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestGetClientForPlatform(t *testing.T) {
	bot, cleanup := setupBotTest(t)
	defer cleanup()
	if bot.clients == nil {
		t.Fatal("clients should not be nil")
	}
	if _, ok := bot.clients[platforms.PlatformGitHub]; !ok {
		t.Error("expected github client")
	}
	if _, ok := bot.clients["unknown"]; ok {
		t.Error("expected no unknown platform client")
	}
}

func TestBotStop(t *testing.T) {
	bot, cleanup := setupBotTest(t)
	defer cleanup()
	bot.Stop()
}
