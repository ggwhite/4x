package prompt

import (
	"strings"

	"github.com/ggwhite/4x/internal/protocol"
)

// FormatProfileArtifactSection 產生 profile-aware 產出物契約段落（英文，與 template 語氣一致）。
// 段落列出當前 profile 名稱、已啟用 phase 流程、本角色必須產出的 artifact，以及（若有）
// 本 profile 其他啟用角色擁有的禁止產出物、與本 profile 不會產出因而缺席屬正常的上游 report。
// 空的 slice 對應行整行省略，輸出 deterministic。
func FormatProfileArtifactSection(profileName string, pc protocol.ProfileConfig, role protocol.Role) string {
	view := pc.RoleArtifactView(role)

	var b strings.Builder
	b.WriteString("== Profile-aware artifact contract ==\n")
	b.WriteString("Active profile: " + profileName + " — phases: " + strings.Join(pc.EnabledPhaseNames(), " → ") + "\n")
	b.WriteString("You MUST produce in this profile: " + strings.Join(view.Required, ", "))

	if len(view.Forbidden) > 0 {
		b.WriteString("\nDo NOT write (owned by other active roles this profile): " + strings.Join(view.Forbidden, ", "))
	}

	if len(view.AbsentUpstream) > 0 {
		b.WriteString("\nThese upstream reports are NOT produced in profile \"" + profileName + "\" — their\n")
		b.WriteString("absence is EXPECTED. Do NOT treat a missing one as a failure or as grounds\n")
		b.WriteString("to reject/FAIL: " + strings.Join(view.AbsentUpstream, ", "))
	}

	return b.String()
}
