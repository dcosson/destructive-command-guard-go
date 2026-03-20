package guard

// Option configures evaluation behavior.
type Option func(*evalConfig)

type evalConfig struct {
	destructivePolicy Policy
	privacyPolicy     Policy
	allowlist         []string
	blocklist         []string
	enabledPacks      []string
	disabledPacks     []string
	callerEnv         []string
}

func defaultConfig() evalConfig {
	return evalConfig{
		destructivePolicy: InteractivePolicy(),
		privacyPolicy:     InteractivePolicy(),
	}
}

func WithDestructivePolicy(p Policy) Option {
	return func(c *evalConfig) {
		c.destructivePolicy = p
	}
}

func WithPrivacyPolicy(p Policy) Option {
	return func(c *evalConfig) {
		c.privacyPolicy = p
	}
}

func WithAllowlist(patterns ...string) Option {
	return func(c *evalConfig) {
		c.allowlist = append(c.allowlist, patterns...)
	}
}

func WithBlocklist(patterns ...string) Option {
	return func(c *evalConfig) {
		c.blocklist = append(c.blocklist, patterns...)
	}
}

func WithPacks(packs ...string) Option {
	return func(c *evalConfig) {
		c.enabledPacks = append([]string{}, packs...)
	}
}

func WithDisabledPacks(packs ...string) Option {
	return func(c *evalConfig) {
		c.disabledPacks = append(c.disabledPacks, packs...)
	}
}

func WithEnv(env []string) Option {
	return func(c *evalConfig) {
		c.callerEnv = env
	}
}
