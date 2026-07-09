package server

import "github.com/ggwhite/4x/internal/protocol"

// ProcessWorkspace 是 ProcessManager 依賴的最小 workspace 介面，只涵蓋啟動/監控子程序需要的方法，
// 讓測試可注入假 workspace 而不需完整 *protocol.Workspace。*protocol.Workspace 已滿足此介面。
type ProcessWorkspace interface {
	AppendEvent(featureID string, evt protocol.Event) error
	ReadState(featureID string) (protocol.State, error)
	WriteState(featureID string, s protocol.State) error
	UpdateState(featureID string, mutate func(s *protocol.State) error) (protocol.State, error)
}

// BatchWorkspace 是 BatchManager 依賴的最小 workspace 介面。
// *protocol.Workspace 已滿足此介面。
type BatchWorkspace interface {
	DotDir() string
	WriteBatchStop() error
	ReadBatchPID() (int, error)
	ClearBatchPID() error
}
