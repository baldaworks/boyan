package semanticeval_test

// This public, synthetic corpus was authored before running the reference model.
// Labels express human relevance judgments, never vectors or observed rankings.
type document struct {
	id, text, subject string
}

var documents = []document{
	{"tea", "Mira prefers unsweetened green tea in the morning.", "mira-drink"},
	{"coffee", "Mira avoids green tea and orders sweet coffee at night.", "mira-coffee"},
	{"bike", "Антон ездит на работу на велосипеде, чтобы не стоять в пробках.", "anton-commute"},
	{"car", "Антон ремонтирует велосипед по выходным, а на работу едет на машине.", "anton-hobby"},
	{
		"backup",
		"Production PostgreSQL backups run nightly at 02:00 UTC; restore drills happen monthly.",
		"database-backup",
	},
	{
		"logs",
		"Production PostgreSQL logs rotate at 02:00 UTC; backups are disabled on this test replica.",
		"database-logs",
	},
	{"allergy", "У Лены аллергия на арахис; блюда с peanut sauce нужно исключить.", "lena-allergy"},
	{"recipe", "Лена записала рецепт peanut sauce для гостей без пищевой аллергии.", "lena-recipe"},
	{"residence", "Nora lives in Bishkek near the botanical garden.", "nora-residence"},
}

type query struct {
	id, text, want string
	topK           int
}

var queries = []query{
	{"en-exact", "unsweetened green tea", "tea", 1},
	{"en-paraphrase", "What beverage does Mira like without sugar at breakfast?", "tea", 3},
	{"ru-exact", "велосипеде пробках", "bike", 1},
	{"ru-paraphrase", "Как Антон добирается до офиса, избегая дорожных заторов?", "bike", 3},
	{"mixed-exact", "PostgreSQL backups nightly", "backup", 1},
	{"mixed-paraphrase", "Когда сохраняют резервные копии production database?", "backup", 3},
	{"mixed-allergy", "Какие продукты нельзя Лене: peanut allergy?", "allergy", 3},
	{"residence-exact", "Nora Bishkek botanical", "residence", 1},
	{"semantic-only", "bicycle commuting", "bike", 3},
}

// Frozen canonical identities bind judgments to exact fixture revisions. They
// were captured from deterministic ingest, independently of model inference.
var revisions = map[string]string{
	"tea":               "boyan-memory:revision:f8e4702834960c8b629f0171ba49b6d6826015f1abab0c9f0f540ffd8548fd20",
	"coffee":            "boyan-memory:revision:efc67dbf7dd0babb77500f94f7adbda8be98a376555b02ba9604fffcad1311f6",
	"bike":              "boyan-memory:revision:f209b59f21eae2dfcbbd6cf2df97af4d655f7e3a0c41a7dbd64fd397d3b73d87",
	"car":               "boyan-memory:revision:cc923674ac3eb8697d7eed4212793f218150436418d5b2f4afe1b10aa39a8ea9",
	"backup":            "boyan-memory:revision:3c27beafa5d2246f0c1bc39dbc4b9f1556a4b48a7028afdeb7447b99f98f6669",
	"logs":              "boyan-memory:revision:ea5cb105b0891965bc48e6afd90711c7c3c1659192354bc2f458e15ec8b3bbf7",
	"allergy":           "boyan-memory:revision:f6dcea49e4155bd7292e5fd27a3753a938528ddb4d70a17543ff74c87deabe4e",
	"recipe":            "boyan-memory:revision:3819777b3872e73700718b131f688d42711738e32b2f30721310436d23304d39",
	"residence":         "boyan-memory:revision:e762b5cfe2b8ff21a9a1f0d85de3d5233785f03f0d71ef39becec9d364b4c669",
	"residence-updated": "boyan-memory:revision:5fbfa5350193a0e70dd73305ee161a1d7d3f17a90af2b23166ad0f8118620138",
}

const (
	referenceModel    = "intfloat/multilingual-e5-base"
	referenceRevision = "d128750597153bb5987e10b1c3493a34e5a4502a"
)
