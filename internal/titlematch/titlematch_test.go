package titlematch

import "testing"

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"Kimetsu no Yaiba!":             "kimetsu no yaiba",
		"Pokémon Adventures":            "pokemon adventures",
		"Solo Leveling (Official)":      "solo leveling",
		"The Beginning After The End":   "the beginning after the end",
		"Tomo-chan wa Onna no Ko!":      "tomo chan wa onna no ko",
		"Ｆｕｌｌｗｉｄｔｈ":                     "fullwidth",
		"Spy×Family":                    "spy family",
		"Tower of God [Colored]":        "tower of god",
		"Kaguya-sama: Love Is War":      "kaguya sama love is war",
		"Frieren: Beyond Journey’s End": "frieren beyond journey s end",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScore(t *testing.T) {
	same := [][2]string{
		{"Solo Leveling", "Solo Leveling (Official)"},
		{"Solo Leveling", "solo-leveling"},
		{"Spy x Family", "SPY×FAMILY"},
		{"The Beginning After the End", "Beginning After The End"},
		{"Kaguya-sama: Love Is War", "Kaguya-sama wa Kokurasetai: Love is War"},
		{"Frieren: Beyond Journey's End", "Frieren: Beyond Journey’s End"},
		{"One Piece", "One Piece"},
		{"Pokémon Adventures", "Pokemon Adventures"},
	}
	for _, p := range same {
		if s := Score(p[0], p[1]); s < 0.88 {
			t.Errorf("Score(%q, %q) = %.2f, want >= 0.88", p[0], p[1], s)
		}
	}
	different := [][2]string{
		{"Solo Leveling", "Solo Leveling: Ragnarok"},
		{"One Piece", "One Punch-Man"},
		{"Tower of God", "God of High School"},
		{"Naruto", "Boruto: Naruto Next Generations"},
		{"Berserk", "Berserk of Gluttony"},
		{"Blue Lock", "Blue Lock: Episode Nagi"},
	}
	for _, p := range different {
		if s := Score(p[0], p[1]); s >= 0.88 {
			t.Errorf("Score(%q, %q) = %.2f, want < 0.88", p[0], p[1], s)
		}
	}
	if Best([]string{"Shingeki no Kyojin", "Attack on Titan"}, "Attack on Titan (Colored)") < 0.95 {
		t.Error("Best should use alternative titles")
	}
}
