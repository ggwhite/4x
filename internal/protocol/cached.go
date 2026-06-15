package protocol

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CachedWorkspace 在 *Workspace 上加一層 mtime-based in-memory cache，供 long-running
// dashboard server 使用，減少每次 API 請求重複 parse 大量 YAML/JSON。
//
// 失效策略完全靠檔案 mtime 比對：每次讀取先用 os.Stat / os.ReadDir 取 metadata，
// 與 cache 記錄的 mtime 相同才回傳 cache，否則重新 parse。因此寫入端（SaveFeature /
// WriteState 等）無需主動通知 cache，下次讀取自動偵測變動。
//
// 為避免 stat→parse 之間檔案被改動造成「cache 記錄的 mtime 與資料版本不一致」
// （存舊 mtime → 永久 miss；存新 mtime 配舊資料 → 回傳 stale），所有 override
// 方法採 stat→parse→stat：唯有 parse 前後兩次 mtime 一致才寫入 cache，不一致則
// 略過 cache（仍回傳這次讀到的正確資料），由下次讀取重試。
//
// 透過 Go embedding 滿足 WorkspaceReader 並沿用 *Workspace 的所有其他方法；
// 僅 ListFeatures / LoadFeature / ReadConfig 被 override 加上 cache。ReadState
// 刻意不 cache（頻繁變化、檔案小、parse 快）。
//
// 注意：Go embedding 無虛擬分派，*Workspace 內部方法呼叫的 w.ListFeatures() 等
// 仍走原版、不命中 cache，這是已知且可接受的限制（那些路徑非 server hot-path）。
type CachedWorkspace struct {
	*Workspace

	mu sync.RWMutex

	configCache *Config
	configMtime time.Time

	featuresCache  []Feature
	featuresMtimes map[string]time.Time // filename → mtime

	featureCache map[string]Feature   // id → Feature
	featureMtime map[string]time.Time // id → mtime
}

var _ WorkspaceReader = (*CachedWorkspace)(nil)

// NewCachedWorkspace 建立一個包裝 ws 的 CachedWorkspace，初始 cache 為空。
func NewCachedWorkspace(ws *Workspace) *CachedWorkspace {
	return &CachedWorkspace{Workspace: ws}
}

// ReadConfig 讀取 .4x/settings.json；settings.json 的 mtime 未變時回傳 cache 副本。
func (c *CachedWorkspace) ReadConfig() (Config, error) {
	path := filepath.Join(c.DotDir(), ConfigFile)
	info, err := os.Stat(path)
	if err != nil {
		return Config{}, err
	}
	mtime := info.ModTime()

	c.mu.RLock()
	if c.configCache != nil && mtime.Equal(c.configMtime) {
		cfg := *c.configCache
		c.mu.RUnlock()
		return cfg, nil
	}
	c.mu.RUnlock()

	cfg, err := c.Workspace.ReadConfig()
	if err != nil {
		return Config{}, err
	}

	// re-stat：parse 期間檔案若被改動，mtime 會變，此時不寫 cache 以免存入不一致版本。
	if info2, err2 := os.Stat(path); err2 == nil && info2.ModTime().Equal(mtime) {
		c.mu.Lock()
		c.configCache = &cfg
		c.configMtime = mtime
		c.mu.Unlock()
	}

	return cfg, nil
}

// LoadMergedConfig 讀取 project config（走 cache 版 ReadConfig）並合併 user config。
// 必須 override 而非沿用 embedded *Workspace 版本——Go embedding 無虛擬分派，
// *Workspace.LoadMergedConfig 內部呼叫的是非 cache 的 w.ReadConfig，會 bypass cache。
// 語意與 *Workspace.LoadMergedConfig 一致：project config 失敗回 error 不做 user merge，
// user config 失敗印 slog.Warn 但不中斷。
func (c *CachedWorkspace) LoadMergedConfig() (Config, error) {
	cfg, err := c.ReadConfig()
	if err != nil {
		return Config{}, err
	}
	if userCfg, err := ReadUserConfig(); err != nil {
		slog.Warn("failed to read user config", "error", err)
	} else {
		cfg = MergeConfig(userCfg, cfg)
	}
	return cfg, nil
}

// ListFeatures 列出所有 feature；features 目錄內容或任一 .yaml 的 mtime 改變時重新 parse。
// 回傳的是 cache 的 shallow copy：slice 結構與 value 欄位已隔離（增刪元素、改 Name 等
// 不影響 cache），但 Feature 的 reference 欄位（Repos / Subtasks / Rules / Depends /
// Hooks / Priority）仍與 cache 共用底層；呼叫端切勿就地修改這些欄位內容，否則會污染
// cache 且不受 c.mu 保護。
func (c *CachedWorkspace) ListFeatures() ([]Feature, error) {
	dir := filepath.Join(c.DotDir(), FeaturesDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	c.mu.RLock()
	if c.featuresCache != nil && yamlMtimesMatch(entries, c.featuresMtimes) {
		result := make([]Feature, len(c.featuresCache))
		copy(result, c.featuresCache)
		c.mu.RUnlock()
		return result, nil
	}
	c.mu.RUnlock()

	mtimes := collectYamlMtimes(entries)

	features, err := c.Workspace.ListFeatures()
	if err != nil {
		return nil, err
	}

	// re-read：parse 期間目錄或任一 yaml 若被改動，重讀的集合與 mtimes 不再吻合，
	// 此時不寫 cache 以免存入不一致版本。
	if entries2, err2 := os.ReadDir(dir); err2 == nil && yamlMtimesMatch(entries2, mtimes) {
		c.mu.Lock()
		c.featuresCache = features
		c.featuresMtimes = mtimes
		c.mu.Unlock()
	}

	result := make([]Feature, len(features))
	copy(result, features)
	return result, nil
}

// collectYamlMtimes 從 dir entries 蒐集所有 .yaml 檔的 filename → mtime 對照表。
func collectYamlMtimes(entries []os.DirEntry) map[string]time.Time {
	mtimes := make(map[string]time.Time)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		if fi, err := e.Info(); err == nil {
			mtimes[e.Name()] = fi.ModTime()
		}
	}
	return mtimes
}

// yamlMtimesMatch 比對 entries 中的 .yaml 集合與 mtime 是否與 mtimes 完全一致。
// 同時驗證「每個 yaml mtime 相同」與「yaml 總數 == mtimes 記錄數」，以偵測新增與刪除。
func yamlMtimesMatch(entries []os.DirEntry, mtimes map[string]time.Time) bool {
	yamlCount := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".yaml" {
			continue
		}
		yamlCount++
		fi, err := e.Info()
		if err != nil {
			return false
		}
		cached, ok := mtimes[e.Name()]
		if !ok || !fi.ModTime().Equal(cached) {
			return false
		}
	}
	return yamlCount == len(mtimes)
}

// LoadFeature 讀取單一 feature；對應 YAML 的 mtime 未變時回傳 cache。
func (c *CachedWorkspace) LoadFeature(id string) (Feature, error) {
	path := filepath.Join(c.DotDir(), FeaturesDir, id+".yaml")
	info, err := os.Stat(path)
	if err != nil {
		return Feature{}, fmt.Errorf("read feature %s: %w", id, err)
	}
	mtime := info.ModTime()

	c.mu.RLock()
	if c.featureCache != nil {
		if cached, ok := c.featureCache[id]; ok {
			if mt, ok := c.featureMtime[id]; ok && mtime.Equal(mt) {
				c.mu.RUnlock()
				return cached, nil
			}
		}
	}
	c.mu.RUnlock()

	f, err := c.Workspace.LoadFeature(id)
	if err != nil {
		return Feature{}, err
	}

	// re-stat：parse 期間檔案若被改動，mtime 會變，此時不寫 cache 以免存入不一致版本。
	if info2, err2 := os.Stat(path); err2 == nil && info2.ModTime().Equal(mtime) {
		c.mu.Lock()
		if c.featureCache == nil {
			c.featureCache = make(map[string]Feature)
			c.featureMtime = make(map[string]time.Time)
		}
		c.featureCache[id] = f
		c.featureMtime[id] = mtime
		c.mu.Unlock()
	}

	return f, nil
}
