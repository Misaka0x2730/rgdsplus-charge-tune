package i18n

import (
	"encoding/json"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

var i *I18N

type I18N struct {
	localizer *i18n.Localizer
	bundle    *i18n.Bundle
}

type MessageFile struct {
	Name    string
	Content []byte
}

func InitI18N(messageFilePaths []string) error {
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	for _, messageFile := range messageFilePaths {
		_, err := bundle.LoadMessageFile(messageFile)
		if err != nil {
			return err
		}
	}

	localizer := i18n.NewLocalizer(bundle, language.English.String(), language.Spanish.String())

	i = &I18N{localizer: localizer, bundle: bundle}

	return nil
}

func InitI18NFromBytes(messageFiles []MessageFile) error {
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)

	for _, messageFile := range messageFiles {
		_, err := bundle.ParseMessageFileBytes(messageFile.Content, messageFile.Name)
		if err != nil {
			return err
		}
	}

	localizer := i18n.NewLocalizer(bundle, language.English.String(), language.Spanish.String())

	i = &I18N{localizer: localizer, bundle: bundle}

	return nil
}

func SetLanguage(lang language.Tag) {
	localizer := i18n.NewLocalizer(i.bundle, lang.String())

	i = &I18N{localizer: localizer, bundle: i.bundle}
}

func SetWithCode(code string) error {
	lang, err := language.Parse(code)
	if err != nil {
		return err
	}
	SetLanguage(lang)
	return nil
}

// GetString retrieves a localized string by key
// If the key is not found, it returns the key itself as fallback
func GetString(key string) string {
	msg, err := i.localizer.Localize(&i18n.LocalizeConfig{
		MessageID: key,
	})
	if err != nil {
		return "I18N Error"
	}
	return msg
}

// GetStringWithData retrieves a localized string by key with template data
// templateData should be a map[string]interface{} containing values for template variables
func GetStringWithData(key string, templateData map[string]interface{}) string {
	msg, err := i.localizer.Localize(&i18n.LocalizeConfig{
		MessageID:    key,
		TemplateData: templateData,
	})
	if err != nil {
		return "I18N Error"
	}
	return msg
}

// GetPluralString retrieves a localized string with plural support
// count determines which plural form to use
func GetPluralString(key string, count int) string {
	msg, err := i.localizer.Localize(&i18n.LocalizeConfig{
		MessageID:   key,
		PluralCount: count,
	})
	if err != nil {
		return "I18N Error"
	}
	return msg
}

// Message is an alias for i18n.Message to avoid requiring users to import go-i18n directly
type Message = i18n.Message

// Localize retrieves a localized string using the go-i18n struct pattern.
// The DefaultMessage provides the message ID and fallback text.
// If a translation exists for the current locale, it will be used; otherwise the default is returned.
//
// Example usage:
//
//	i18n.Localize(&i18n.Message{
//	    ID:    "greeting",
//	    Other: "Hello, World!",
//	}, nil)
//
// With template data:
//
//	i18n.Localize(&i18n.Message{
//	    ID:    "welcome_user",
//	    Other: "Welcome, {{.Name}}!",
//	}, map[string]interface{}{"Name": "Alice"})
func Localize(message *Message, templateData map[string]interface{}) string {
	if message == nil {
		return "I18N Error: nil message"
	}

	config := &i18n.LocalizeConfig{
		DefaultMessage: message,
	}

	if templateData != nil {
		config.TemplateData = templateData
	}

	msg, err := i.localizer.Localize(config)
	if err != nil {
		// Fallback to the default message's Other field
		if message.Other != "" {
			return message.Other
		}
		return "I18N Error"
	}
	return msg
}

// LocalizePlural retrieves a localized string with plural support using the struct pattern.
// The count determines which plural form to use (One, Few, Many, Other, etc.)
//
// Example usage:
//
//	i18n.LocalizePlural(&i18n.Message{
//	    ID:    "items_count",
//	    One:   "{{.Count}} item",
//	    Other: "{{.Count}} items",
//	}, 5, map[string]interface{}{"Count": 5})
func LocalizePlural(message *Message, count int, templateData map[string]interface{}) string {
	if message == nil {
		return "I18N Error: nil message"
	}

	config := &i18n.LocalizeConfig{
		DefaultMessage: message,
		PluralCount:    count,
	}

	if templateData != nil {
		config.TemplateData = templateData
	}

	msg, err := i.localizer.Localize(config)
	if err != nil {
		// Fallback to the default message
		if message.Other != "" {
			return message.Other
		}
		return "I18N Error"
	}
	return msg
}
