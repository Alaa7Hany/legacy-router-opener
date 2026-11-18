package bridgeui

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"strings"
)

//go:embed messages.json
var messagesJSON []byte

var messages = loadMessages()

func loadMessages() map[string]map[string]string {
	var catalog map[string]map[string]string
	if err := json.Unmarshal(messagesJSON, &catalog); err != nil {
		panic(err)
	}
	return catalog
}

// SelectLanguage uses an explicit UI language or the highest-ranked supported browser language.
func SelectLanguage(explicit, acceptLanguage string) string {
	if explicit == "ar" || explicit == "en" {
		return explicit
	}
	language, bestQuality := "en", 0.0
	for entry := range strings.SplitSeq(acceptLanguage, ",") {
		tag, parameters, _ := strings.Cut(entry, ";")
		base, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
		if base != "ar" && base != "en" {
			continue
		}
		quality := 1.0
		if key, value, ok := strings.Cut(strings.TrimSpace(parameters), "="); ok && key == "q" {
			var err error
			quality, err = strconv.ParseFloat(value, 64)
			if err != nil {
				continue
			}
		}
		if quality > bestQuality && quality <= 1 {
			language, bestQuality = base, quality
		}
	}
	return language
}

func Text(language, key string) string {
	return messages[language][key]
}

type pageLanguage struct {
	Language  string
	Direction string
	Messages  map[string]string
	Catalog   map[string]map[string]string
}

func forLanguage(language string) pageLanguage {
	page := pageLanguage{
		Language: language, Direction: "ltr", Messages: messages[language], Catalog: messages,
	}
	if language == "ar" {
		page.Direction = "rtl"
	}
	return page
}
