package main

import "testing"

func TestMediaWorkerConfigDisabledWithoutSecretsByDefault(t *testing.T) {
	config, err := loadConfig(func(string) string { return "" })
	if err != nil || config.enabled {
		t.Fatalf("loadConfig() = %#v, %v; want disabled config", config, err)
	}
}

func TestMediaWorkerConfigRequiresDatabaseAndBucketScopedStorageCredentials(t *testing.T) {
	values := map[string]string{"MEDIA_SCAN_ENABLED": "true"}
	getenv := func(key string) string { return values[key] }
	if _, err := loadConfig(getenv); err == nil {
		t.Fatal("loadConfig() accepted enabled worker without database URL")
	}
	values["DATABASE_URL"] = "postgres://media:test@localhost:5432/media?sslmode=disable"
	if _, err := loadConfig(getenv); err == nil {
		t.Fatal("loadConfig() accepted enabled worker without R2 credentials")
	}
	values["R2_ACCOUNT_ID"] = "0123456789abcdef0123456789abcdef"
	values["R2_BUCKET"] = "city4christ-media"
	values["MEDIA_R2_ACCESS_KEY_ID"] = "worker-access"
	values["MEDIA_R2_SECRET_ACCESS_KEY"] = "worker-secret"
	config, err := loadConfig(getenv)
	if err != nil {
		t.Fatalf("loadConfig() rejected complete config: %v", err)
	}
	if !config.enabled || config.clamAVBinary != "clamdscan" || config.pollInterval <= 0 || config.leaseDuration <= config.pollInterval {
		t.Fatalf("loadConfig() = %#v", config)
	}
}

func TestMediaWorkerConfigRejectsInvalidEnableFlagAndPartialR2Config(t *testing.T) {
	values := map[string]string{"MEDIA_SCAN_ENABLED": "yes"}
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil {
		t.Fatal("loadConfig() accepted invalid enable flag")
	}
	values = map[string]string{
		"MEDIA_SCAN_ENABLED": "true", "DATABASE_URL": "postgres://media:test@localhost:5432/media?sslmode=disable",
		"R2_ACCOUNT_ID": "0123456789abcdef0123456789abcdef",
	}
	if _, err := loadConfig(func(key string) string { return values[key] }); err == nil {
		t.Fatal("loadConfig() accepted incomplete R2 settings")
	}
}
