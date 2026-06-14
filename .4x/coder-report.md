# Coder Report — Deep Review Fixes

## Fixes applied

### Issue 1: Phantom runner entries (business-logic)

**Root cause:** `collectFormData()` used `querySelectorAll('[id^="ps-runner-"]')` on `ps-runners-wrap`. CSS attribute selectors without scope restrictions match all descendants, including child inputs (`ps-runner-cmd-claude`, `ps-runner-model-claude`, `ps-runner-stdin-claude`, etc.). Each matched child element had its ID stripped of the `ps-runner-` prefix, producing phantom runner names like `cmd-claude`, `model-claude`, `stdin-claude` in the runners payload.

**Fix:** Changed to `:scope > [id^="ps-runner-"]` so only direct children of `ps-runners-wrap` are matched (the runner card `<div>` elements).

**File:** `internal/server/static/index.html` line 851

---

### Issue 2: Cannot clear optional fields (business-logic)

**Root cause:** `setIfNonEmpty` dropped empty-string values for `description` and `language` from the payload. Backend `mergeSettings()` preserves existing keys absent from incoming, so the old value survived silently.

**Fix:** Removed `setIfNonEmpty` for `description` and `language`. These fields are now always included in the payload (even as empty string), allowing `mergeSettings` to overwrite the old value. The `name` field retains the required-only-if-non-empty semantics. No backend change needed — `mergeSettings` already handles `""` as an overwrite correctly.

**File:** `internal/server/static/index.html` lines 824–828

---

### Issue 3: Spec deviations (spec-mismatch)

**Root cause:** Spec (F027) specifies `Cmd+,` opens the project settings (`.4x/settings.json` editor). Implementation had `Cmd+,` open the appearance panel and `Cmd+Shift+,` open project settings.

**Fixes:**
1. **Keyboard shortcut:** Swapped assignments — `Cmd+,` now opens project settings, `Cmd+Shift+,` opens appearance settings.
2. **Auto-save on blur (spec required):** Added `autoSave()` function with 300ms debounce. Wired to `onblur` on all text inputs, `onchange` on all selects/checkboxes, and called from `addTag`, `removeTag`, `addRunner`, `removeRunner`. Shows a brief "Saved" indicator (`ps-autosave-ind`) that fades after 1.5s.
3. **Search/filter (spec required):** Added `ps-search-bar` with search input above the form. `filterPSFields()` hides/shows `.ps-field-row` elements by matching `data-label` and `data-key` attributes case-insensitively. Each `psField` and `psTagField` now renders with these data attributes. Search bar is hidden in JSON mode.

**Files:** `internal/server/static/index.html`

---

## Verification

```
go build ./cmd/4x  → success
go vet ./...       → no issues
go test ./...      → 256 passed
```
