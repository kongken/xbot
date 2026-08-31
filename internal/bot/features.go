package bot

import telegram "github.com/go-telegram/bot"

type handlerRegistrar interface {
	RegisterHandler(
		handlerType telegram.HandlerType,
		pattern string,
		matchType telegram.MatchType,
		handler telegram.HandlerFunc,
		middlewares ...telegram.Middleware,
	) string
}

func registerFeatureHandlers(registrar handlerRegistrar, features featureSet, memory *memoryService) {
	if features.has(featureAssistant) {
		registerAssistantHandlers(registrar, memory)
	}
	if features.has(featurePoll) {
		registerPollHandlers(registrar)
	}
	if features.has(featureUtility) {
		registerUtilityHandlers(registrar)
	}
}

func registerAssistantHandlers(registrar handlerRegistrar, memory *memoryService) {
	registrar.RegisterHandler(telegram.HandlerTypeMessageText, "/gpt", telegram.MatchTypePrefix, gptHandler)
	registrar.RegisterHandler(telegram.HandlerTypeMessageText, "gpt", telegram.MatchTypePrefix, gptHandler)
	registrar.RegisterHandler(telegram.HandlerTypeMessageText, "/chat", telegram.MatchTypePrefix, chatHandler)
	registrar.RegisterHandler(telegram.HandlerTypeMessageText, "/sum", telegram.MatchTypePrefix, sumHandler)
	registrar.RegisterHandler(telegram.HandlerTypeMessageText, "/ask", telegram.MatchTypePrefix, askHandler)
	registrar.RegisterHandler(telegram.HandlerTypeMessageText, "/huahua", telegram.MatchTypePrefix, huahuaHandler)
	registrar.RegisterHandler(telegram.HandlerTypeMessageText, "/save_prompt", telegram.MatchTypePrefix, savePromt)
	registrar.RegisterHandler(telegram.HandlerTypeMessageText, "/hualao", telegram.MatchTypeExact, hualaoHandler)
	registrar.RegisterHandler(telegram.HandlerTypeMessageText, "/poster", telegram.MatchTypeExact, posterHandler)
	if memory != nil {
		registrar.RegisterHandler(telegram.HandlerTypeMessageText, "/memory", telegram.MatchTypePrefix, memory.commandHandler())
	}
}

func registerPollHandlers(registrar handlerRegistrar) {
	for _, config := range pollConfig {
		registrar.RegisterHandler(
			telegram.HandlerTypeMessageText,
			config.Command,
			telegram.MatchTypePrefix,
			newPollHandler(config),
		)
	}
}

func registerUtilityHandlers(registrar handlerRegistrar) {
	registrar.RegisterHandler(telegram.HandlerTypeMessageText, "/hello", telegram.MatchTypePrefix, helloHandler)
	registrar.RegisterHandler(telegram.HandlerTypeMessageText, "/dns_query", telegram.MatchTypePrefix, dnsQueryHandler)
	registrar.RegisterHandler(telegram.HandlerTypeMessageText, "/getid", telegram.MatchTypeExact, getIDHandler)
	registrar.RegisterHandler(telegram.HandlerTypeMessageText, "/me", telegram.MatchTypeExact, meHandler)
	registrar.RegisterHandler(telegram.HandlerTypeMessageText, "/set", telegram.MatchTypePrefix, setKeywordHandler)
}
