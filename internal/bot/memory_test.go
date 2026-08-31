package bot

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	telegram "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"go.orx.me/xbot/internal/conf"
	"go.orx.me/xbot/internal/dao"
	"go.orx.me/xbot/internal/pkg/mem0"
)

func testMem0Config() conf.Mem0 {
	cfg := (conf.Mem0{Endpoint: "https://m.example.com", APIKey: "k"}).Effective()
	cfg.FlushInterval = conf.Duration(30 * time.Second)
	cfg.MaxAttempts = 3
	return cfg
}

func buildUpdate(chatID int64, chatType models.ChatType, from *models.User, text, caption string) *models.Update {
	msg := &models.Message{
		ID:   1,
		Date: int(time.Now().Unix()),
		Chat: models.Chat{ID: chatID, Type: chatType},
		From: from,
		Text: text,
	}
	if caption != "" {
		msg.Caption = caption
	}
	return &models.Update{ID: 7, Message: msg}
}

func user(id int64, name string) *models.User {
	return &models.User{ID: id, FirstName: name}
}

func TestRegisterAssistantHandlersAddsMemoryCommand(t *testing.T) {
	m, _, _ := newTestService()
	registrar := &recordingRegistrar{}
	registerAssistantHandlers(registrar, m)
	if !slices.Contains(registrar.patterns, "/memory") {
		t.Fatalf("registered patterns = %v, want /memory", registrar.patterns)
	}
}

func TestNormalize_GroupMessage(t *testing.T) {
	m, _, _ := newTestService()
	event := m.normalize(buildUpdate(-1001, models.ChatTypeSupergroup, user(99, "Alice"), "hello world", ""))
	if event == nil {
		t.Fatal("expected event")
	}
	if event.ChatID != -1001 || event.SenderID != 99 || event.Content != "hello world" {
		t.Fatalf("unexpected event: %+v", event)
	}
	if event.DedupKey != "assistant:7" {
		t.Fatalf("dedup key = %q", event.DedupKey)
	}
	if event.SenderLabel != "Alice" {
		t.Fatalf("sender label = %q", event.SenderLabel)
	}
}

func TestNormalize_CaptionAndEdit(t *testing.T) {
	m, _, _ := newTestService()

	upd := buildUpdate(-1001, models.ChatTypeGroup, user(1, "Bob"), "", "photo caption")
	if event := m.normalize(upd); event == nil || event.Content != "photo caption" {
		t.Fatalf("caption event = %+v", event)
	}

	edited := &models.Update{ID: 8, EditedMessage: &models.Message{
		ID: 1, Date: int(time.Now().Unix()),
		Chat: models.Chat{ID: -1001, Type: models.ChatTypeSupergroup},
		From: user(1, "Bob"), Text: "edited",
	}}
	event := m.normalize(edited)
	if event == nil || !event.IsEdit {
		t.Fatalf("edited event = %+v", event)
	}
}

func TestNormalize_SkipsIneligible(t *testing.T) {
	m, _, _ := newTestService()
	tests := []struct {
		name string
		upd  *models.Update
	}{
		{"private chat", buildUpdate(111, models.ChatTypePrivate, user(1, "A"), "hi", "")},
		{"channel", buildUpdate(-100, models.ChatTypeChannel, user(1, "A"), "hi", "")},
		{"not in allowlist", buildUpdate(-9999, models.ChatTypeSupergroup, user(1, "A"), "hi", "")},
		{"command", buildUpdate(-1001, models.ChatTypeSupergroup, user(1, "A"), "/gpt hi", "")},
		{"bot authored", buildUpdate(-1001, models.ChatTypeSupergroup, &models.User{ID: 2, IsBot: true}, "hi", "")},
		{"empty content", buildUpdate(-1001, models.ChatTypeSupergroup, user(1, "A"), "", "")},
		{"no message", &models.Update{ID: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if event := m.normalize(test.upd); event != nil {
				t.Fatalf("expected nil, got %+v", event)
			}
		})
	}
}

func TestShouldFlushEvents(t *testing.T) {
	cfg := testMem0Config()
	now := time.Now().Unix()
	recent := &dao.Mem0Event{MessageDate: now, Content: "x"}
	old := &dao.Mem0Event{MessageDate: now - 60, Content: "old"}

	if _, ok := shouldFlushEvents(nil, cfg); ok {
		t.Fatal("expected no flush for empty")
	}
	if _, ok := shouldFlushEvents([]*dao.Mem0Event{recent}, cfg); ok {
		t.Fatal("expected no flush for small recent batch")
	}

	// Count threshold.
	var twenty []*dao.Mem0Event
	for i := 0; i < 20; i++ {
		twenty = append(twenty, recent)
	}
	if picked, ok := shouldFlushEvents(twenty, cfg); !ok || len(picked) != 20 {
		t.Fatal("expected flush at count threshold")
	}

	// Time threshold.
	if _, ok := shouldFlushEvents([]*dao.Mem0Event{old}, cfg); !ok {
		t.Fatal("expected flush for old message")
	}

	// Byte threshold.
	big := &dao.Mem0Event{MessageDate: now, Content: string(make([]byte, cfg.MaxBatchBytes+1))}
	if _, ok := shouldFlushEvents([]*dao.Mem0Event{big}, cfg); !ok {
		t.Fatal("expected byte-threshold flush to return true")
	}
}

func TestFilterDue(t *testing.T) {
	now := time.Now().Unix()
	events := []*dao.Mem0Event{
		{NextAttemptAt: now - 1},
		{NextAttemptAt: now + 3600},
	}
	due := filterDue(events)
	if len(due) != 1 || due[0].NextAttemptAt != now-1 {
		t.Fatalf("filterDue = %+v", due)
	}
}

func TestFlushGroup_SendsBatchAndMarksProcessed(t *testing.T) {
	m, outbox, client := newTestService()
	outbox.chats[-1001] = []*dao.Mem0Event{
		{DedupKey: "assistant:1", BotName: "assistant", ChatID: -1001, SenderID: 99, SenderLabel: "Alice", Content: "hello", MessageDate: time.Now().Unix() - 60},
	}

	m.flushGroup(context.Background(), -1001)

	if len(client.addCalls) != 1 {
		t.Fatalf("add calls = %d, want 1", len(client.addCalls))
	}
	req := client.addCalls[0]
	if req.AgentID != "xbot:assistant" || req.RunID != "telegram-chat:-1001" {
		t.Fatalf("ids = %+v", req)
	}
	if len(req.Messages) != 1 || req.Messages[0].Content != "[telegram-user:99, Alice] hello" {
		t.Fatalf("messages = %+v", req.Messages)
	}
	if len(outbox.markGroup) != 1 || outbox.markGroup[0][0] != "assistant:1" {
		t.Fatalf("markGroup = %+v", outbox.markGroup)
	}
}

func TestFlushGroup_RetriesAndDeadLetters(t *testing.T) {
	cfg := testMem0Config()
	cfg.MaxAttempts = 2

	newService := func(attempts int) (*memoryService, *fakeOutbox, *fakeClient) {
		m, outbox, client := newTestServiceWith(cfg)
		outbox.chats[-1001] = []*dao.Mem0Event{
			{DedupKey: "k1", ChatID: -1001, Content: "a", Attempts: attempts, MessageDate: time.Now().Unix() - 60},
		}
		client.addErr = errors.New("nope")
		return m, outbox, client
	}

	t.Run("records failure below max", func(t *testing.T) {
		m, outbox, _ := newService(0)
		m.flushGroup(context.Background(), -1001)
		if len(outbox.recordFail) != 1 || outbox.recordFail[0] != "k1" {
			t.Fatalf("recordFail = %+v", outbox.recordFail)
		}
		if len(outbox.dead) != 0 {
			t.Fatalf("dead = %+v, want none", outbox.dead)
		}
	})

	t.Run("dead letters at max attempts", func(t *testing.T) {
		m, outbox, _ := newService(cfg.MaxAttempts)
		m.flushGroup(context.Background(), -1001)
		if len(outbox.dead) != 1 || outbox.dead[0] != "k1" {
			t.Fatalf("dead = %+v", outbox.dead)
		}
		if len(outbox.recordFail) != 0 {
			t.Fatalf("recordFail = %+v, want none", outbox.recordFail)
		}
	})
}

func TestTick_FlushesProfiles(t *testing.T) {
	m, outbox, client := newTestService()
	outbox.chats[-1001] = []*dao.Mem0Event{
		{DedupKey: "k1", ChatID: -1001, SenderID: 42, Content: "hi", MessageDate: time.Now().Unix() - 60},
	}
	outbox.senders = []int64{42}
	outbox.profileEvents[42] = []*dao.Mem0Event{
		{DedupKey: "k1", ChatID: -1001, SenderID: 42, Content: "hi", MessageDate: time.Now().Unix() - 60},
	}

	m.tick(context.Background())

	// Group projection + user projection.
	if len(client.addCalls) != 2 {
		t.Fatalf("add calls = %d, want 2", len(client.addCalls))
	}
	var userCall *mem0.AddRequest
	for i := range client.addCalls {
		if client.addCalls[i].UserID == "telegram-user:42" {
			userCall = &client.addCalls[i]
		}
	}
	if userCall == nil {
		t.Fatal("expected a user profile projection")
	}
	if userCall.AgentID != "xbot:assistant" || userCall.RunID != "telegram-chat:-1001" {
		t.Fatalf("user projection ids = %+v", userCall)
	}
}

func TestRateLimit(t *testing.T) {
	m, _, _ := newTestService()
	if !m.rateLimit(-1001, user(1, "A"), "profile") {
		t.Fatal("first call should pass")
	}
	if m.rateLimit(-1001, user(1, "A"), "profile") {
		t.Fatal("second call should be rate limited")
	}
	if !m.rateLimit(-1001, user(1, "A"), "fresh") {
		t.Fatal("different subcommand should pass")
	}
}

func TestParseFreshWindow(t *testing.T) {
	m, _, _ := newTestService()
	cases := []struct {
		in   string
		want time.Duration
	}{
		{"", mem0DefaultFresh},
		{"24h", 24 * time.Hour},
		{"3d", 72 * time.Hour},
		{"7d", 168 * time.Hour},
		{"30d", mem0MaxFreshWindow},
		{"garbage", mem0DefaultFresh},
	}
	for _, c := range cases {
		if got := m.parseFreshWindow([]string{c.in}); got != c.want {
			t.Fatalf("parseFreshWindow(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestMiddleware_AlwaysCallsNext(t *testing.T) {
	m, _, _ := newTestService()
	b, err := telegram.New("1:token", telegram.WithSkipGetMe())
	if err != nil {
		t.Fatalf("telegram.New() error = %v", err)
	}

	called := false
	middleware := m.middleware()
	next := func(_ context.Context, _ *telegram.Bot, _ *models.Update) {
		called = true
	}
	middleware(next)(context.Background(), b, buildUpdate(-1001, models.ChatTypeSupergroup, user(1, "A"), "group message", ""))

	if !called {
		t.Fatal("next handler was not called")
	}
}

// ---------------------------------------------------------------------------
// Fakes and helpers
// ---------------------------------------------------------------------------

func newTestService() (*memoryService, *fakeOutbox, *fakeClient) {
	return newTestServiceWith(testMem0Config())
}

func newTestServiceWith(cfg conf.Mem0) (*memoryService, *fakeOutbox, *fakeClient) {
	outbox := &fakeOutbox{
		chats:         make(map[int64][]*dao.Mem0Event),
		profileEvents: make(map[int64][]*dao.Mem0Event),
	}
	client := &fakeClient{}
	return newMemoryService("assistant", []int64{-1001}, outbox, client, cfg), outbox, client
}

type fakeOutbox struct {
	mu            sync.Mutex
	chats         map[int64][]*dao.Mem0Event
	profileEvents map[int64][]*dao.Mem0Event
	senders       []int64
	recent        []*dao.Mem0Event
	markGroup     [][]string
	markProfile   [][]string
	recordFail    []string
	dead          []string
	saveErr       error
}

func (f *fakeOutbox) Save(_ context.Context, event *dao.Mem0Event) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chats[event.ChatID] = append(f.chats[event.ChatID], event)
	f.profileEvents[event.SenderID] = append(f.profileEvents[event.SenderID], event)
	return nil
}

func (f *fakeOutbox) MarkGroupProcessed(_ context.Context, _ string, _ int64, keys []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markGroup = append(f.markGroup, keys)
	return nil
}

func (f *fakeOutbox) MarkProfileProcessed(_ context.Context, _ string, _ int64, keys []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markProfile = append(f.markProfile, keys)
	return nil
}

func (f *fakeOutbox) MarkDead(_ context.Context, keys []string, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dead = append(f.dead, keys...)
	return nil
}

func (f *fakeOutbox) RecordFailure(_ context.Context, keys []string, _ int64, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recordFail = append(f.recordFail, keys...)
	return nil
}

func (f *fakeOutbox) PendingByChat(_ context.Context, _ string, chatID int64, _ int) ([]*dao.Mem0Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.chats[chatID], nil
}

func (f *fakeOutbox) PendingByChatSender(_ context.Context, _ string, _ int64, senderID int64, _ int) ([]*dao.Mem0Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.profileEvents[senderID], nil
}

func (f *fakeOutbox) PendingProfileSenders(_ context.Context, _ string, _ int64) ([]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.senders, nil
}

func (f *fakeOutbox) RecentEvents(_ context.Context, _ string, _ int64, _, _ int64, _ int) ([]*dao.Mem0Event, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recent, nil
}

type fakeClient struct {
	mu           sync.Mutex
	addErr       error
	addCalls     []mem0.AddRequest
	searchErr    error
	searchCalls  []mem0.SearchRequest
	searchResult []mem0.Memory
}

func (f *fakeClient) Add(_ context.Context, req mem0.AddRequest) (mem0.AddResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addCalls = append(f.addCalls, req)
	if f.addErr != nil {
		return mem0.AddResult{}, f.addErr
	}
	return mem0.AddResult{}, nil
}

func (f *fakeClient) Search(_ context.Context, req mem0.SearchRequest) ([]mem0.Memory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.searchCalls = append(f.searchCalls, req)
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.searchResult, nil
}
