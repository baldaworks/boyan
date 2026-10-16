package sqlite

import (
	"strings"
	"unicode"

	"github.com/kljensen/snowball/english"
	"github.com/kljensen/snowball/russian"
	"golang.org/x/text/unicode/norm"
)

// lexicalFields is shared by indexing and querying; raw retains stopwords and
// tokens from every script. Snowball's stopword option is consistently false.
type lexicalFields struct{ raw, ru, en string }

func analyzeLexicalText(text string) lexicalFields {
	tokens := strings.FieldsFunc(strings.ToLower(norm.NFC.String(text)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	var ru, en []string

	for _, token := range tokens {
		cyrillic, latin := false, false
		for _, r := range token {
			cyrillic = cyrillic || unicode.In(r, unicode.Cyrillic)
			latin = latin || unicode.In(r, unicode.Latin)
		}

		if cyrillic {
			ru = append(ru, russian.Stem(token, false))
		}

		if latin {
			en = append(en, english.Stem(token, false))
		}
	}

	return lexicalFields{raw: strings.Join(tokens, " "), ru: strings.Join(ru, " "), en: strings.Join(en, " ")}
}

func lexicalMatch(text string) string {
	fields := analyzeLexicalText(text)

	var clauses []string

	for _, field := range []struct{ name, value string }{
		{"raw_tokens", fields.raw}, {"ru_tokens", fields.ru}, {"en_tokens", fields.en},
	} {
		for _, token := range strings.Fields(field.value) {
			clauses = append(clauses, field.name+`:"`+strings.ReplaceAll(token, `"`, `""`)+`"`)
		}
	}

	return strings.Join(clauses, " OR ")
}
