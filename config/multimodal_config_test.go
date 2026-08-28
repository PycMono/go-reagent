package config

import "testing"

func TestLoadConfigWiresVisionToPlatformOptions(t *testing.T) {
	path := writeConfig(t, `{
		"currentPlatform":"vision-p",
		"platforms":[
			{"id":"vision-p","protocol":"anthropic","baseURL":"https://x.test/","apiKey":"k","model":"m","vision":true,
			 "pricing":{"input_usd_per_million_tokens":0,"output_usd_per_million_tokens":0}},
			{"id":"plain-p","protocol":"openai","baseURL":"https://y.test/","apiKey":"k","model":"m",
			 "pricing":{"input_usd_per_million_tokens":0,"output_usd_per_million_tokens":0}}
		],
		"agent":{"limits":{"max_turns":5}},
		"redis":{"addr":["127.0.0.1:6379"]}
	}`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Platforms) != 2 {
		t.Fatalf("platforms = %d, want 2", len(cfg.Platforms))
	}
	if !cfg.Platforms[0].Vision {
		t.Fatalf("vision-p Vision = false, want true (config vision must reach providers.Options)")
	}
	if cfg.Platforms[1].Vision {
		t.Fatalf("plain-p Vision = true, want default false")
	}

	options, err := cfg.CurrentPlatformOptions()
	if err != nil {
		t.Fatal(err)
	}
	if !options.Vision {
		t.Fatalf("CurrentPlatformOptions Vision = false, want true")
	}
}
