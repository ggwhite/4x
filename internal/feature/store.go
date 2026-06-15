package feature

// Store 抽象化 feature 的持久化操作，由 protocol.Workspace 與 protocol.CachedWorkspace
// 隱式實作。feature package 透過此 interface 與 protocol 解耦，避免 protocol → feature →
// protocol 的循環依賴。
type Store interface {
	DotDir() string
	FeatureDir(featureID string) string
	RoundDir(featureID string, round int) string
	SaveFeature(f Feature) error
	LoadFeature(id string) (Feature, error)
	ListFeatures() ([]Feature, error)
	InitFeatureDir(featureID string) error
	ResolveFeatureID(prefix string) (string, error)
}
