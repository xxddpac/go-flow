package zlog

const (
	FormatText = "text"
	FormatJSON = "json"
)

type Config struct {
	LogPath    string
	LogLevel   string
	Compress   bool
	MaxSize    int
	MaxAge     int
	MaxBackups int
	Format     string
}

func (c *Config) fillWithDefault() {
	if c.MaxSize <= 0 {
		c.MaxSize = 10
	}
	if c.MaxAge <= 0 {
		c.MaxAge = 7
	}
	if c.MaxBackups <= 0 {
		c.MaxBackups = 3
	}
	if c.LogLevel == "" {
		c.LogLevel = "debug"
	}
	if c.Format == "" {
		c.Format = FormatText
	}
}
