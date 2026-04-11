package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/cihann/juliterm/internal/config"
	"github.com/cihann/juliterm/internal/tmux"
)

// writeJSONError writes a JSON {"error": msg} response with the given status code.
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// writeJSONOK writes a JSON {"ok": true} response with the given status code.
func writeJSONOK(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// mapTmuxError inspects a tmux command error and returns an appropriate HTTP
// status code. Common tmux errors include "can't find session" (404) and
// "duplicate session" (409).
func mapTmuxError(err error) int {
	var ve *tmux.ValidationError
	if errors.As(err, &ve) {
		return http.StatusBadRequest
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "duplicate session"):
		return http.StatusConflict
	case strings.Contains(msg, "can't find"),
		strings.Contains(msg, "session not found"),
		strings.Contains(msg, "window not found"),
		strings.Contains(msg, "no such session"),
		strings.Contains(msg, "no such window"):
		return http.StatusNotFound
	case strings.Contains(msg, "pane too small"),
		strings.Contains(msg, "create pane failed"):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

// --- Session handlers ---

type nameRequest struct {
	Name string `json:"name"`
}

// handleCreateSession handles POST /api/sessions.
func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	var req nameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := s.tmuxMgr.CreateSession(r.Context(), req.Name); err != nil {
		writeJSONError(w, mapTmuxError(err), err.Error())
		return
	}
	writeJSONOK(w, http.StatusCreated)
}

// handleRenameSession handles PATCH /api/sessions/{name}.
func (s *Server) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	oldName := r.PathValue("name")
	if oldName == "" {
		writeJSONError(w, http.StatusBadRequest, "session name is required")
		return
	}
	var req nameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "new name is required")
		return
	}
	if err := s.tmuxMgr.RenameSession(r.Context(), oldName, req.Name); err != nil {
		writeJSONError(w, mapTmuxError(err), err.Error())
		return
	}
	writeJSONOK(w, http.StatusOK)
}

// handleKillSession handles DELETE /api/sessions/{name}.
func (s *Server) handleKillSession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "session name is required")
		return
	}
	if err := s.tmuxMgr.KillSession(r.Context(), name); err != nil {
		writeJSONError(w, mapTmuxError(err), err.Error())
		return
	}
	writeJSONOK(w, http.StatusOK)
}

// --- Window handlers ---

// handleCreateWindow handles POST /api/sessions/{session}/windows.
func (s *Server) handleCreateWindow(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("session")
	if sessionName == "" {
		writeJSONError(w, http.StatusBadRequest, "session name is required")
		return
	}
	if err := s.tmuxMgr.CreateWindow(r.Context(), sessionName); err != nil {
		writeJSONError(w, mapTmuxError(err), err.Error())
		return
	}
	writeJSONOK(w, http.StatusCreated)
}

// handleRenameWindow handles PATCH /api/sessions/{session}/windows/{index}.
func (s *Server) handleRenameWindow(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("session")
	indexStr := r.PathValue("index")
	if sessionName == "" || indexStr == "" {
		writeJSONError(w, http.StatusBadRequest, "session name and window index are required")
		return
	}
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "window index must be an integer")
		return
	}
	var req nameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "new name is required")
		return
	}
	if err := s.tmuxMgr.RenameWindow(r.Context(), sessionName, index, req.Name); err != nil {
		writeJSONError(w, mapTmuxError(err), err.Error())
		return
	}
	writeJSONOK(w, http.StatusOK)
}

// handleKillWindow handles DELETE /api/sessions/{session}/windows/{index}.
func (s *Server) handleKillWindow(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("session")
	indexStr := r.PathValue("index")
	if sessionName == "" || indexStr == "" {
		writeJSONError(w, http.StatusBadRequest, "session name and window index are required")
		return
	}
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "window index must be an integer")
		return
	}
	if err := s.tmuxMgr.KillWindow(r.Context(), sessionName, index); err != nil {
		writeJSONError(w, mapTmuxError(err), err.Error())
		return
	}
	writeJSONOK(w, http.StatusOK)
}

// --- Split pane handlers ---

type splitRequest struct {
	Preset    string `json:"preset"`
	Command   string `json:"command,omitempty"`
	Direction string `json:"direction,omitempty"` // "horizontal" or "vertical" (custom preset only)
}

// presetJSON is the JSON shape returned by GET /api/presets.
type presetJSON struct {
	Name      string `json:"name"`
	Label     string `json:"label"`
	Direction string `json:"direction"`
	Percent   int    `json:"percent"`
	Detach    bool   `json:"detach"`
}

// handleGetPresets handles GET /api/sessions/{session}/presets.
func (s *Server) handleGetPresets(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("session")

	builtIn := []presetJSON{
		{Name: "lazygit", Label: "lazygit", Direction: "horizontal", Percent: 50},
		{Name: "test-runner", Label: "Run Tests", Direction: "vertical", Percent: 30, Detach: true},
	}

	global := splitPresetsToJSON(s.cfg.Splits)

	var project []presetJSON
	if cwd, _ := s.tmuxMgr.PaneCwd(r.Context(), sessionName); cwd != "" {
		if path := config.FindProjectConfig(cwd); path != "" {
			if projectCfg, err := config.LoadFile(path); err == nil {
				project = splitPresetsToJSON(projectCfg.Splits)
			} else {
				log.Printf("WARNING: invalid .juli.yaml at %s: %v", path, err)
			}
		}
	}

	// Remove global presets overridden by project (same name).
	if len(project) > 0 {
		projectNames := make(map[string]bool, len(project))
		for _, p := range project {
			projectNames[p.Name] = true
		}
		filtered := global[:0]
		for _, g := range global {
			if !projectNames[g.Name] {
				filtered = append(filtered, g)
			}
		}
		global = filtered
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"builtIn": builtIn,
		"global":  global,
		"project": project,
	})
}

func splitPresetsToJSON(presets []config.SplitPreset) []presetJSON {
	if len(presets) == 0 {
		return nil
	}
	out := make([]presetJSON, len(presets))
	for i, sp := range presets {
		out[i] = presetJSON{
			Name:      sp.Name,
			Label:     sp.Name,
			Direction: sp.Direction,
			Percent:   sp.Percent,
			Detach:    sp.Detach,
		}
	}
	return out
}

func findPreset(name string, presets []config.SplitPreset) (config.SplitPreset, bool) {
	for _, sp := range presets {
		if sp.Name == name {
			return sp, true
		}
	}
	return config.SplitPreset{}, false
}

// handleSplitWindow handles POST /api/sessions/{session}/split.
func (s *Server) handleSplitWindow(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("session")
	if sessionName == "" {
		writeJSONError(w, http.StatusBadRequest, "session name is required")
		return
	}

	var req splitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var opts tmux.SplitOpts
	switch req.Preset {
	case "lazygit":
		opts = tmux.SplitOpts{Horizontal: true, Percent: 50, Command: "lazygit", Detach: false}
	case "test-runner":
		cmd, _ := s.tmuxMgr.DetectTestCommand(r.Context(), sessionName)
		opts = tmux.SplitOpts{Horizontal: false, Percent: 30, Command: cmd, Detach: true}
	case "custom":
		opts = tmux.SplitOpts{Horizontal: req.Direction != "vertical", Percent: 50, Command: req.Command, Detach: false}
	default:
		// Look up user-configured presets (project config takes priority over global).
		var sp config.SplitPreset
		var ok bool
		if cwd, _ := s.tmuxMgr.PaneCwd(r.Context(), sessionName); cwd != "" {
			if path := config.FindProjectConfig(cwd); path != "" {
				if projectCfg, err := config.LoadFile(path); err == nil {
					sp, ok = findPreset(req.Preset, projectCfg.Splits)
				}
			}
		}
		if !ok {
			sp, ok = findPreset(req.Preset, s.cfg.Splits)
		}
		if !ok {
			writeJSONError(w, http.StatusBadRequest, "unknown preset")
			return
		}
		opts = tmux.SplitOpts{
			Horizontal: sp.Direction == "horizontal",
			Percent:    sp.Percent,
			Command:    sp.Command,
			Detach:     sp.Detach,
		}
	}

	if err := s.tmuxMgr.SplitWindow(r.Context(), sessionName, opts); err != nil {
		writeJSONError(w, mapTmuxError(err), err.Error())
		return
	}
	writeJSONOK(w, http.StatusOK)
}

// handleDetectTest handles GET /api/sessions/{session}/detect-test.
func (s *Server) handleDetectTest(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("session")
	if sessionName == "" {
		writeJSONError(w, http.StatusBadRequest, "session name is required")
		return
	}

	cmd, err := s.tmuxMgr.DetectTestCommand(r.Context(), sessionName)
	if err != nil {
		writeJSONError(w, mapTmuxError(err), err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if cmd == "" {
		json.NewEncoder(w).Encode(map[string]any{"command": nil})
	} else {
		json.NewEncoder(w).Encode(map[string]any{"command": cmd})
	}
}

// handleKillPane handles DELETE /api/sessions/{session}/pane.
func (s *Server) handleKillPane(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("session")
	if sessionName == "" {
		writeJSONError(w, http.StatusBadRequest, "session name is required")
		return
	}
	if err := s.tmuxMgr.KillPane(r.Context(), sessionName); err != nil {
		writeJSONError(w, mapTmuxError(err), err.Error())
		return
	}
	writeJSONOK(w, http.StatusOK)
}

// handleZoomPane handles POST /api/sessions/{session}/zoom.
func (s *Server) handleZoomPane(w http.ResponseWriter, r *http.Request) {
	sessionName := r.PathValue("session")
	if sessionName == "" {
		writeJSONError(w, http.StatusBadRequest, "session name is required")
		return
	}
	if err := s.tmuxMgr.ZoomPane(r.Context(), sessionName); err != nil {
		writeJSONError(w, mapTmuxError(err), err.Error())
		return
	}
	writeJSONOK(w, http.StatusOK)
}
