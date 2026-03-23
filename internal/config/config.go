package config

import (
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	Redis     RedisConfig     `yaml:"redis"`
	LLM       LLMConfig       `yaml:"llm"`
	Cache     CacheConfig     `yaml:"cache"`
	RateLimit RateLimitConfig `yaml:"ratelimit"`
	WeChat    WeChatConfig    `yaml:"wechat"`
	Cron      CronConfig      `yaml:"cron"`
}

type ServerConfig struct {
	Port           int `yaml:"port"`
	TimeoutSeconds int `yaml:"timeout_seconds"`
}

type DatabaseConfig struct {
	DSN          string `yaml:"dsn"`
	MaxOpenConns int    `yaml:"max_open_conns"`
	MaxIdleConns int    `yaml:"max_idle_conns"`
}

type RedisConfig struct {
	Addr     string `yaml:"addr"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
	PoolSize int    `yaml:"pool_size"`
}

type LLMConfig struct {
	Provider       string `yaml:"provider"`
	APIKey         string `yaml:"api_key"`
	APIURL         string `yaml:"api_url"`
	Model          string `yaml:"model"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
	EmbeddingURL   string `yaml:"embedding_url"`
	EmbeddingModel string `yaml:"embedding_model"`
	EmbeddingDim   int    `yaml:"embedding_dim"`
}

type CacheConfig struct {
	TTLSeconds int `yaml:"ttl_seconds"`
}

type RateLimitConfig struct {
	RequestsPerMinute int `yaml:"requests_per_minute"`
}

type WeChatConfig struct {
	AppID              string `yaml:"app_id"`
	AppSecret          string `yaml:"app_secret"`
	SubscribeTemplateID string `yaml:"subscribe_template_id"`
}

type CronConfig struct {
	MatchIntervalMinutes     int     `yaml:"match_interval_minutes"`
	MatchSimilarityThreshold float64 `yaml:"match_similarity_threshold"`
}

var (
	once     sync.Once
	instance *Config
)

func Load(path string) (*Config, error) {
	var err error
	once.Do(func() {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			err = readErr
			return
		}
		instance = &Config{}
		if unmarshalErr := yaml.Unmarshal(data, instance); unmarshalErr != nil {
			err = unmarshalErr
			instance = nil
		}
	})
	return instance, err
}

func Get() *Config {
	return instance
}
