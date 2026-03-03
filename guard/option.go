package guard

// Option configures evaluation behavior.
type Option func(*evalConfig)

type evalConfig struct {
	policy        Policy
	allowlist     []string
	blocklist     []string
	enabledPacks  []string
	disabledPacks []string
	callerEnv     []string
}

func defaultConfig() evalConfig {
	return evalConfig{
		policy: InteractivePolicy(),
	}
}

func WithPolicy(p Policy) Option {
	return func(c *evalConfig) {
		c.policy = p
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
