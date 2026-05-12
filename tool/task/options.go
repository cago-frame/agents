package task

// Option 是 5 个 task_* 工具共用的构造选项。
type Option func(*config)

type config struct {
	store  *Store
	serial bool
}

func newConfig(opts ...Option) *config {
	cfg := &config{}
	for _, o := range opts {
		o(cfg)
	}
	if cfg.store == nil {
		cfg.store = NewStore()
	}
	return cfg
}

// WithStore 让工具挂到外部传入的 *Store 上（同一 Store 内 5 个工具共享状态）。
// 零值时每个 New* 会各自新建一个匿名 Store（外部观察不到）。
func WithStore(s *Store) Option { return func(c *config) { c.store = s } }

// Serial 让该工具与同一轮的其他工具串行执行。
func Serial() Option { return func(c *config) { c.serial = true } }
