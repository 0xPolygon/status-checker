package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/antithesishq/antithesis-sdk-go/assert"
	"github.com/go-playground/validator"
	"github.com/go-viper/mapstructure/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

var metric = prometheus.NewCounterVec(
	prometheus.CounterOpts{Name: "status_check"},
	[]string{"check", "success"},
)

func init() {
	prometheus.MustRegister(metric)
}

type Check struct {
	Enabled  *bool          `mapstructure:"enabled"`
	Interval *time.Duration `mapstructure:"interval"`
}

type Logs struct {
	Pretty bool   `mapstructure:"pretty"`
	Level  string `mapstructure:"level"`
}

type Config struct {
	EnabledByDefault  bool             `mapstructure:"enabled_by_default"`
	Interval          time.Duration    `mapstructure:"interval" validate:"required"`
	Logs              Logs             `mapstructure:"logs"`
	ChecksDir         string           `mapstructure:"checks_dir" validate:"required,dir"`
	Checks            map[string]Check `mapstructure:"checks"`
	ModifyPermissions bool             `mapstructure:"modify_permissions"`
	PromPort          uint             `mapstructure:"prom_port" validate:"required"`
	Antithesis        bool             `mapstructure:"antithesis"`
}

// expandEnvHookFunc expands environment variables when the viper is decoding
// into the `Config` struct.
func expandEnvHookFunc() mapstructure.DecodeHookFunc {
	return func(f reflect.Type, t reflect.Type, data any) (any, error) {
		if f.Kind() == reflect.String && t.Kind() == reflect.String {
			return os.ExpandEnv(data.(string)), nil
		}
		return data, nil
	}
}

func blockFor(duration time.Duration, path string) {
	now := time.Now()
	target := now.Add(duration / 2).Round(duration)

	log.Trace().
		Time("now", now).
		Time("until", target).
		Str("path", path).
		Msg("Blocking")

	timer := time.NewTimer(time.Until(target))
	defer timer.Stop()
	<-timer.C
}

func loadConfig() (*Config, error) {
	v := viper.NewWithOptions(viper.KeyDelimiter("::"))
	v.SetConfigName("config")
	v.AddConfigPath("/etc/status-checker/")
	v.AddConfigPath(".")

	if len(os.Args) > 1 {
		v.SetConfigFile(os.Args[1])
	}

	v.AutomaticEnv()
	v.SetEnvPrefix("status_checker")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	v.SetDefault("enabled_by_default", true)
	v.SetDefault("interval", "30s")
	v.SetDefault("checks_dir", "./checks")
	v.SetDefault("logs::pretty", false)
	v.SetDefault("logs::level", "info")
	v.SetDefault("modify_permissions", false)
	v.SetDefault("prom_port", 9090)

	if err := v.ReadInConfig(); err != nil {
		log.Warn().Msg("No config file found, using defaults")
	}

	opts := viper.DecodeHook(
		mapstructure.ComposeDecodeHookFunc(
			expandEnvHookFunc(),
			mapstructure.StringToTimeDurationHookFunc(),
		),
	)

	var cfg Config
	if err := v.Unmarshal(&cfg, opts); err != nil {
		return nil, err
	}

	validate := validator.New()
	if err := validate.Struct(cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func discoverChecks(dir string, chmod bool) ([]string, error) {
	var files []string

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		logger := log.With().Str("path", path).Logger()

		if err != nil {
			logger.Error().Err(err).Msg("Failed to walk dir")
			return nil
		}

		if d.IsDir() {
			logger.Debug().Msg("Skipping directory")
			return nil
		}

		if strings.HasPrefix(d.Name(), "_") {
			logger.Debug().Msg("Skipping underscored file")
			return nil
		}

		info, err := d.Info()
		if err != nil {
			logger.Warn().Err(err).Msg("Failed to get file info")
			return nil
		}

		if !info.Mode().IsRegular() {
			logger.Debug().Msg("Skipping non-regular file")
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			logger.Warn().Err(err).Msg("Failed to open file")
			return nil
		}
		defer f.Close()

		reader := bufio.NewReader(f)
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil
		}

		if strings.HasPrefix(line, "#!") {
			files = append(files, path)
		}

		if !chmod {
			return nil
		}

		if err := os.Chmod(path, info.Mode()|0111); err != nil {
			logger.Warn().Err(err).Msg("Failed to add executable permission")
		}

		return nil
	})

	return files, err
}

func runCheck(path, check string) bool {
	cmd := exec.Command(path)
	cmd.Env = os.Environ()
	logger := log.With().Str("check", check).Logger()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get pipe")
		return false
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		logger.Error().Err(err).Msg("Failed to start command")
		return false
	}

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		logger.Debug().Msg(line)
	}

	if err := scanner.Err(); err != nil {
		logger.Warn().Err(err).Msg("Failed to read output")
	}

	if err := cmd.Wait(); err != nil {
		logger.Error().Err(err).Send()
		return false
	}

	return true
}

func runCheckLoop(path, check string, interval time.Duration, antithesis bool, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		ok := runCheck(path, check)

		log.Info().Str("check", check).Bool("success", ok).Send()
		metric.WithLabelValues(check, strconv.FormatBool(ok)).Inc()

		if antithesis {
			details := map[string]any{"check": check, "success": ok}
			assert.Always(ok, "check run succeeded", details)
		}

		blockFor(interval, check)
	}
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to load config")
	}

	level, err := zerolog.ParseLevel(cfg.Logs.Level)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse log level")
	}
	zerolog.SetGlobalLevel(level)

	if cfg.Logs.Pretty {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		log.Info().Msg("Starting Prometheus")

		if err := http.ListenAndServe(fmt.Sprint(":", cfg.PromPort), nil); err != nil {
			log.Error().Err(err).Msg("Failed to start Prometheus")
		}
	}()

	checks, err := discoverChecks(cfg.ChecksDir, cfg.ModifyPermissions)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to discover checks")
	}

	if len(checks) == 0 {
		log.Warn().Str("checks_dir", cfg.ChecksDir).Msg("No checks found")
		return
	}

	log.Info().Msg("Starting status-checker")

	var wg sync.WaitGroup
	for _, path := range checks {
		name, err := filepath.Rel(cfg.ChecksDir, path)
		if err != nil {
			log.Warn().Str("check", path).Msg("Failed to get relative path")
			continue
		}

		check, ok := cfg.Checks[name]
		if !ok {
			check = Check{
				Enabled:  &cfg.EnabledByDefault,
				Interval: &cfg.Interval,
			}
		}

		if check.Enabled == nil {
			check.Enabled = &cfg.EnabledByDefault
		}
		if check.Interval == nil {
			check.Interval = &cfg.Interval
		}

		if !*check.Enabled {
			log.Debug().Str("check", name).Msg("Skipping disabled check")
			continue
		}

		wg.Add(1)
		go runCheckLoop(path, name, *check.Interval, cfg.Antithesis, &wg)
	}

	wg.Wait()
}
