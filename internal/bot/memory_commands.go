package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"butterfly.orx.me/core/log"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"go.orx.me/xbot/internal/conf"
	"go.orx.me/xbot/internal/dao"
	"go.orx.me/xbot/internal/metrics"
	"go.orx.me/xbot/internal/pkg/mem0"
)

// commandHandler returns the /memory command handler for this bot.
func (m *memoryService) commandHandler() bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, update *models.Update) {
		m.handleCommand(ctx, b, update)
	}
}

func (m *memoryService) handleCommand(ctx context.Context, b *bot.Bot, update *models.Update) {
	if update.Message == nil {
		return
	}
	chatID := update.Message.Chat.ID

	if !m.enabled(chatID) {
		m.reply(ctx, b, update, "该群未启用记忆功能，无法使用 /memory。", false)
		return
	}

	raw := strings.TrimSpace(strings.TrimPrefix(update.Message.Text, mem0CommandPrefix))
	fields := strings.Fields(raw)
	subcommand := ""
	if len(fields) > 0 {
		subcommand = strings.ToLower(fields[0])
	}

	if !m.rateLimit(update.Message.Chat.ID, update.Message.From, subcommand) {
		m.reply(ctx, b, update, "操作过于频繁，请一分钟后再试。", false)
		return
	}

	switch subcommand {
	case mem0ProfileCommand:
		m.profile(ctx, b, update, chatID)
	case mem0FreshCommand:
		m.fresh(ctx, b, update, chatID, fields[1:])
	default:
		m.reply(ctx, b, update,
			"用法：\n`/memory profile` — 分析你的记忆画像\n"+
				"`/memory fresh [24h|3d|7d]` — 总结群内最近动态", true)
	}
}

func (m *memoryService) profile(ctx context.Context, b *bot.Bot, update *models.Update, chatID int64) {
	from := update.Message.From
	if from == nil || from.IsBot {
		m.reply(ctx, b, update, "请以普通成员身份使用 /memory profile。", false)
		return
	}
	metrics.Mem0CommandTotal.WithLabelValues(mem0ProfileCommand, "attempt").Inc()

	filters := map[string]string{
		"user_id":  mem0UserID(from.ID),
		"agent_id": "xbot:" + m.botName,
		"run_id":   mem0RunID(chatID),
	}

	start := time.Now()
	memories, err := m.client.Search(ctx, mem0.SearchRequest{
		Query:   "用户画像：兴趣、偏好、擅长领域、承诺和常聊话题",
		Filters: filters,
		TopK:    m.cfg.TopK,
	})
	outcome := outcomeSuccess
	if err != nil {
		outcome = outcomeError
	}
	metrics.Mem0RequestDuration.WithLabelValues("search", outcome).Observe(time.Since(start).Seconds())
	metrics.Mem0CommandTotal.WithLabelValues(mem0ProfileCommand, outcome).Inc()

	if err != nil {
		log.FromContext(ctx).Error("mem0 profile search failed", "bot", m.botName, "error", err)
		m.reply(ctx, b, update, "⚠️ 暂时无法获取记忆分析，请稍后再试。", false)
		return
	}
	if len(memories) == 0 {
		m.reply(ctx, b, update,
			"目前还没有关于你的足够记忆。多在群里聊聊（话题、偏好、决定等）后再试。", false)
		return
	}

	input := memoriesToText(memories)
	systemPrompt := `
你是用户记忆画像分析助手。根据下列长期记忆中该用户的相关观察，生成一份简明、中立的画像。
要求：
1. 覆盖可观察到的兴趣、偏好、擅长领域、常参与的话题、承诺事项和交流特点。
2. 区分「已观察到的事实」和「不确定的推测」，推测要明确标注。
3. 证据不足的项目不要编造；没有足够信息时直接说明。
4. 不要推断健康状况、政治/宗教立场、性取向或心理诊断等敏感信息。`

	result, model, err := m.summarize(ctx, systemPrompt, input)
	if err != nil {
		log.FromContext(ctx).Error("mem0 profile summarize failed", "bot", m.botName, "error", err)
		m.reply(ctx, b, update, "⚠️ 画像生成失败，请稍后再试。", false)
		return
	}

	text := fmt.Sprintf(
		"🧠 *记忆画像*\n以下内容基于你在本群的历史记忆生成，群内可见。\n\nModel: `%s`\n\n%s",
		model, result)
	m.reply(ctx, b, update, text, true)
}

func (m *memoryService) fresh(ctx context.Context, b *bot.Bot, update *models.Update, chatID int64, argParts []string) {
	window := m.parseFreshWindow(argParts)
	metrics.Mem0CommandTotal.WithLabelValues(mem0FreshCommand, "attempt").Inc()

	now := time.Now()
	start := time.Now()
	events, err := m.outbox.RecentEvents(ctx, m.botName, chatID, now.Add(-window).Unix(), now.Unix(), mem0FreshMaxEvents)
	outcome := outcomeSuccess
	if err != nil {
		outcome = outcomeError
	}
	metrics.Mem0CommandTotal.WithLabelValues(mem0FreshCommand, outcome).Inc()
	if err != nil {
		log.FromContext(ctx).Error("mem0 fresh events failed", "bot", m.botName, "error", err)
		m.reply(ctx, b, update, "⚠️ 无法读取最近消息，请稍后再试。", false)
		return
	}
	if len(events) == 0 {
		m.reply(ctx, b, update, fmt.Sprintf("最近 %s 内没有可总结的消息。", formatWindow(window)), false)
		return
	}

	// Optional long-term context from Mem0; failure is harmless.
	var memContext []mem0.Memory
	if mems, memErr := m.client.Search(ctx, mem0.SearchRequest{
		Query: "最近重要的讨论、决定、承诺和未解决问题",
		Filters: map[string]string{
			"agent_id": "xbot:" + m.botName,
			"run_id":   mem0RunID(chatID),
		},
		TopK: 5,
	}); memErr == nil {
		memContext = mems
	}
	metrics.Mem0RequestDuration.WithLabelValues("search", outcomeSuccess).Observe(time.Since(start).Seconds())

	input := buildFreshInput(events, memContext, window)
	systemPrompt := `
你是群聊消息总结助手。根据下面最近的消息记录（按时间顺序），总结这个群里最近发生的新鲜事。
请用以下结构输出（使用中文）：
1. 🗓️ 活跃话题：最近讨论的主要话题
2. ✅ 新决定/进展：新拍板的事项或重要进展
3. 📌 承诺与负责人：谁承诺了什么、谁是负责人
4. ❓ 未解决的问题：仍在讨论或悬而未决的问题
5. 💡 新信息：值得关注的新消息
要求：只基于提供的材料总结，不要编造；如果某类没有内容就写「无」。`

	result, model, err := m.summarize(ctx, systemPrompt, input)
	if err != nil {
		log.FromContext(ctx).Error("mem0 fresh summarize failed", "bot", m.botName, "error", err)
		m.reply(ctx, b, update, "⚠️ 总结生成失败，请稍后再试。", false)
		return
	}

	text := fmt.Sprintf("🗞️ *群内新鲜事*（最近 %s，覆盖 %d 条消息）\n\nModel: `%s`\n\n%s",
		formatWindow(window), len(events), model, result)
	m.reply(ctx, b, update, text, true)
}

func (m *memoryService) parseFreshWindow(parts []string) time.Duration {
	window := mem0DefaultFresh
	if len(parts) == 0 {
		return window
	}
	raw := strings.ToLower(strings.TrimSpace(parts[0]))
	multiplier := int64(1)
	switch {
	case strings.HasSuffix(raw, "d"):
		multiplier = 24
		raw = strings.TrimSuffix(raw, "d")
	case strings.HasSuffix(raw, "h"):
		raw = strings.TrimSuffix(raw, "h")
	default:
		return window
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return window
	}
	parsed := time.Duration(n*multiplier) * time.Hour
	if parsed > mem0MaxFreshWindow {
		parsed = mem0MaxFreshWindow
	}
	return parsed
}

func formatWindow(d time.Duration) string {
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	}
	return fmt.Sprintf("%dh", int(d/time.Hour))
}

func memoriesToText(memories []mem0.Memory) string {
	var sb strings.Builder
	for _, memory := range memories {
		if strings.TrimSpace(memory.Memory) == "" {
			continue
		}
		sb.WriteString("- ")
		sb.WriteString(memory.Memory)
		sb.WriteString("\n")
	}
	return sb.String()
}

func buildFreshInput(events []*dao.Mem0Event, memContext []mem0.Memory, window time.Duration) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("最近 %s 的消息记录：\n", formatWindow(window)))
	for _, event := range events {
		sb.WriteString(fmt.Sprintf("[%s] %s\n", event.SenderLabel, event.Content))
	}
	if len(memContext) > 0 {
		sb.WriteString("\n（补充的长期记忆背景）\n")
		sb.WriteString(memoriesToText(memContext))
	}
	return sb.String()
}

func (m *memoryService) rateLimit(chatID int64, from *models.User, subcommand string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	var userID int64
	if from != nil {
		userID = from.ID
	}
	key := fmt.Sprintf("%d:%d:%s", chatID, userID, subcommand)
	now := time.Now()
	if last, ok := m.rate[key]; ok && now.Sub(last) < mem0RateLimitDuration {
		return false
	}
	m.rate[key] = now
	return true
}

func summaryModels() []string {
	models := conf.Conf.SummaryModels
	if len(models) == 0 {
		models = []string{conf.Conf.OpenAI.Model}
	}
	return models
}

func (m *memoryService) reply(ctx context.Context, b *bot.Bot, update *models.Update, text string, markdown bool) {
	params := &bot.SendMessageParams{ChatID: update.Message.Chat.ID, Text: text}
	if markdown {
		params.ParseMode = models.ParseModeMarkdown
	} else {
		params.ParseMode = models.ParseModeMarkdown
		params.Text = bot.EscapeMarkdown(text)
	}
	if _, err := b.SendMessage(ctx, params); err != nil {
		log.FromContext(ctx).Error("mem0 reply failed", "bot", m.botName, "error", err)
	}
}
