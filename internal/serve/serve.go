package serve

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/core"
	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/db"
	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/mcp"
	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/rawg"
)

// Server holds shared state for the HTTP explorer.
type Server struct {
	MCP *mcp.Server
}

// NewServer creates a new HTTP server backed by the given MCP server.
func NewServer(mcpSrv *mcp.Server) *Server {
	return &Server{MCP: mcpSrv}
}

var (
	pageTpl   = template.Must(template.ParseFS(templateFS, "templates/page.html"))
	tagsTpl   = template.Must(template.ParseFS(templateFS, "templates/tags.html"))
	gameTpl   = template.Must(template.ParseFS(templateFS, "templates/game.html"))
	vizTpl    = template.Must(template.ParseFS(templateFS, "templates/viz.html"))
	aboutTpl  = template.Must(template.ParseFS(templateFS, "templates/about.html"))
	viewsTpl  = template.Must(template.ParseFS(templateFS, "templates/views.html"))
	reviewTpl = template.Must(template.ParseFS(templateFS, "templates/review.html"))
)

type pageData struct {
	Games     []db.GameEntry
	TagFilter string
	Query     string
}

type vizPoint struct {
	Name     string   `json:"name"`
	Platform string   `json:"platform"`
	X        float32  `json:"x"`
	Y        float32  `json:"y"`
	Tags     []string `json:"tags"`
	XLabel   string   `json:"xLabel"`
	YLabel   string   `json:"yLabel"`
}

type vizData struct {
	XTag          string
	YTag          string
	PointsJSON    template.JS
	TagsJSON      template.JS
	PlatformsJSON template.JS
}

type gamePageData struct {
	Game    db.GameEntry
	Meta    *db.RAWGMeta
	ShowArt bool
}

// Serve sets up the HTTP mux and starts listening.
func (s *Server) Serve(addr string) error {
	mux := http.NewServeMux()
	s.registerMCPRoutes(mux)
	mux.HandleFunc("/", s.handleGames)
	mux.HandleFunc("/tags", s.handleTags)
	mux.HandleFunc("/game/", s.handleGame)
	mux.HandleFunc("/api/health", s.handleAPIHealth)
	mux.HandleFunc("/api/games", s.handleAPIGames)
	mux.HandleFunc("/api/games/", s.handleAPIGame)
	mux.HandleFunc("/api/tags", s.handleAPITags)
	mux.HandleFunc("/api/playlists", s.handleAPIPlaylists)
	mux.HandleFunc("/api/playlists/", s.handleAPIPlaylist)
	mux.HandleFunc("/api/views", s.handleAPIViews)
	mux.HandleFunc("/api/review", s.handleAPIReview)
	mux.HandleFunc("/viz", s.handleViz)
	mux.HandleFunc("/viz/save", s.handleVizSave)
	mux.HandleFunc("/views", s.handleViews)
	mux.HandleFunc("/review", s.handleReview)
	mux.HandleFunc("/review/correct", s.handleReviewCorrect)
	mux.HandleFunc("/review/skip", s.handleReviewSkip)
	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		aboutTpl.Execute(w, nil)
	})
	return http.ListenAndServe(addr, mux)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func apiError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]interface{}{
		"error": map[string]interface{}{
			"status":  status,
			"message": msg,
		},
	})
}

func filterBrowsableGames(q, tagFilter, platform string, verifiedOnly bool) []db.GameEntry {
	q = strings.ToLower(q)
	games, _ := db.ListGames(platform)

	if verifiedOnly {
		validKeys, _ := db.ListCachedKeys()
		validSet := make(map[string]bool, len(validKeys))
		for _, k := range validKeys {
			validSet[k] = true
		}
		filtered := games[:0]
		for _, g := range games {
			if isSystemFile(g.Name) {
				continue
			}
			if !validSet[db.CacheKey(g.Name)] {
				continue
			}
			filtered = append(filtered, g)
		}
		games = filtered
	}

	sort.Slice(games, func(i, j int) bool { return games[i].Name < games[j].Name })

	if tagFilter != "" {
		filtered := games[:0]
		for _, g := range games {
			for _, t := range g.Tags {
				if t == tagFilter {
					filtered = append(filtered, g)
					break
				}
			}
		}
		games = filtered
	}

	if q != "" {
		filtered := games[:0]
		for _, g := range games {
			if strings.Contains(strings.ToLower(g.Name), q) {
				filtered = append(filtered, g)
				continue
			}
			for _, t := range g.Tags {
				if strings.Contains(strings.ToLower(t), q) {
					filtered = append(filtered, g)
					break
				}
			}
		}
		games = filtered
	}

	return games
}

// isSystemFile returns true for ROM collection metadata files that sneak into
// the games table (e.g. "sega-game-gear-romset-ultra-us_meta.sqlite").
func isSystemFile(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range []string{".sqlite", ".db", ".xml", ".dat", ".txt"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func isTailscale(r *http.Request) bool {
	return r.Header.Get("X-Tailscale") == "1"
}

// ── HTML handlers ────────────────────────────────────────────────────────────

func (s *Server) handleGames(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(r.URL.Query().Get("q"))
	tagFilter := r.URL.Query().Get("tag")
	games := filterBrowsableGames(q, tagFilter, "", true)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	pageTpl.Execute(w, pageData{Games: games, TagFilter: tagFilter, Query: r.URL.Query().Get("q")})
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	rows, _ := db.TagCounts()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	tagsTpl.Execute(w, rows)
}

func (s *Server) handleGame(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/game/")
	name = strings.ReplaceAll(name, "+", " ")
	if n, err := url.QueryUnescape(name); err == nil {
		name = n
	}
	if name == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	entry, err := db.GetGame(name)
	if err != nil || entry == nil {
		http.NotFound(w, r)
		return
	}
	meta, _ := db.GetCachedMeta(db.CacheKey(entry.Name))
	showArt := strings.HasSuffix(r.Host, ".onion")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	gameTpl.Execute(w, gamePageData{Game: *entry, Meta: meta, ShowArt: showArt})
}

// computeVizPoints computes the scatter-plot data for two lighthouse tags.
func computeVizPoints(xTag, yTag string) ([]vizPoint, error) {
	xVec, xOK := db.GetTagEmbedding(xTag)
	yVec, yOK := db.GetTagEmbedding(yTag)
	if !xOK || !yOK {
		return nil, nil
	}

	// Build valid game set (same filter as /games).
	validKeys, _ := db.ListCachedKeys()
	validSet := make(map[string]bool, len(validKeys))
	for _, k := range validKeys {
		validSet[k] = true
	}

	gameEmbeds, err := db.GetAllGameEmbeddingsForViz()
	if err != nil {
		return nil, err
	}

	games, _ := db.ListGames("")
	tagMap := make(map[string][]string, len(games))
	platMap := make(map[string]string, len(games))
	for _, g := range games {
		if isSystemFile(g.Name) || !validSet[db.CacheKey(g.Name)] {
			continue
		}
		tagMap[g.Name] = g.Tags
		platMap[g.Name] = g.Platform
	}

	var points []vizPoint
	for name, vec := range gameEmbeds {
		if _, ok := tagMap[name]; !ok {
			continue // system file or unverified
		}
		points = append(points, vizPoint{
			Name:     name,
			Platform: platMap[name],
			X:        core.CosineSim(vec, xVec),
			Y:        core.CosineSim(vec, yVec),
			Tags:     tagMap[name],
			XLabel:   xTag,
			YLabel:   yTag,
		})
	}
	return points, nil
}

func (s *Server) handleViz(w http.ResponseWriter, r *http.Request) {
	xTag := r.URL.Query().Get("x")
	yTag := r.URL.Query().Get("y")
	if xTag == "" {
		xTag = "action-forward"
	}
	if yTag == "" {
		yTag = "chill"
	}

	points, _ := computeVizPoints(xTag, yTag)

	var js []byte
	if len(points) > 0 {
		js, _ = json.Marshal(points)
	} else {
		js = []byte("[]")
	}

	// Build tag name list for autocomplete — only tags from verified games.
	tagSet := make(map[string]struct{})
	for i := range points {
		for _, t := range points[i].Tags {
			tagSet[t] = struct{}{}
		}
	}
	tagNames := make([]string, 0, len(tagSet))
	for t := range tagSet {
		tagNames = append(tagNames, t)
	}
	sort.Strings(tagNames)
	tagsJS, _ := json.Marshal(tagNames)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	platFilter := r.URL.Query().Get("platforms")
	platJS := template.JS("null")
	if platFilter != "" {
		platJS = template.JS(platFilter)
	}
	vizTpl.Execute(w, vizData{XTag: xTag, YTag: yTag, PointsJSON: template.JS(js), TagsJSON: template.JS(tagsJS), PlatformsJSON: platJS})
}

func (s *Server) handleVizSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Name      string   `json:"name"`
		XTag      string   `json:"x_tag"`
		YTag      string   `json:"y_tag"`
		Platforms []string `json:"platforms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.XTag == "" || req.YTag == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	points, err := computeVizPoints(req.XTag, req.YTag)
	if err != nil {
		http.Error(w, "embed error", http.StatusInternalServerError)
		return
	}
	if points == nil {
		http.Error(w, "unknown tag", http.StatusBadRequest)
		return
	}

	js, _ := json.Marshal(points)
	platJS, _ := json.Marshal(req.Platforms)
	if err := db.SaveView(req.Name, req.XTag, req.YTag, string(js), string(platJS)); err != nil {
		http.Error(w, "save error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleViews(w http.ResponseWriter, r *http.Request) {
	views, _ := db.ListView()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	viewsTpl.Execute(w, views)
}

func (s *Server) handleReview(w http.ResponseWriter, r *http.Request) {
	if !isTailscale(r) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	queue, _ := db.ListReviewQueue()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	reviewTpl.Execute(w, queue)
}

func (s *Server) handleReviewCorrect(w http.ResponseWriter, r *http.Request) {
	if !isTailscale(r) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.ParseForm()
	gameName := r.FormValue("game")
	rawgIDStr := r.FormValue("rawg_id")
	if gameName == "" || rawgIDStr == "" {
		http.Error(w, "missing fields", http.StatusBadRequest)
		return
	}
	var rawgID int
	if _, err := fmt.Sscanf(rawgIDStr, "%d", &rawgID); err != nil || rawgID <= 0 {
		http.Error(w, "invalid rawg_id", http.StatusBadRequest)
		return
	}
	if _, err := rawg.FetchGameMetadataByID(gameName, rawgID); err != nil {
		http.Error(w, "RAWG fetch failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	db.RecordCorrection(gameName, rawgID, "")
	http.Redirect(w, r, "/review", http.StatusSeeOther)
}

func (s *Server) handleReviewSkip(w http.ResponseWriter, r *http.Request) {
	if !isTailscale(r) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	r.ParseForm()
	gameName := r.FormValue("game")
	if gameName == "" {
		http.Error(w, "missing game", http.StatusBadRequest)
		return
	}
	db.MarkSkip(gameName, "manually skipped")
	http.Redirect(w, r, "/review", http.StatusSeeOther)
}

// ── API handlers ─────────────────────────────────────────────────────────────

func (s *Server) handleAPIHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"service": "rom-tagger",
	})
}

func (s *Server) handleAPIGames(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	q := r.URL.Query().Get("q")
	tag := r.URL.Query().Get("tag")
	platform := r.URL.Query().Get("platform")
	verifiedOnly := r.URL.Query().Get("verified") != "0"
	games := filterBrowsableGames(q, tag, platform, verifiedOnly)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"games":    games,
		"count":    len(games),
		"query":    q,
		"tag":      tag,
		"platform": platform,
		"verified": verifiedOnly,
	})
}

func (s *Server) handleAPIGame(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/games/")
	if n, err := url.QueryUnescape(name); err == nil {
		name = n
	}
	if name == "" {
		apiError(w, http.StatusBadRequest, "missing game name")
		return
	}
	entry, err := db.GetGame(name)
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entry == nil {
		apiError(w, http.StatusNotFound, "game not found")
		return
	}
	meta, _ := db.GetCachedMeta(db.CacheKey(entry.Name))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"game": entry,
		"meta": meta,
	})
}

func (s *Server) handleAPITags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	rows, err := db.TagCounts()
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tags":  rows,
		"count": len(rows),
	})
}

func (s *Server) handleAPIPlaylists(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	playlists, err := db.ListPlaylists()
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"playlists": playlists,
		"count":     len(playlists),
	})
}

func (s *Server) handleAPIPlaylist(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/playlists/")
	if n, err := url.QueryUnescape(name); err == nil {
		name = n
	}
	if name == "" {
		apiError(w, http.StatusBadRequest, "missing playlist name")
		return
	}
	entry, games, err := db.GetPlaylist(name)
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if entry == nil {
		apiError(w, http.StatusNotFound, "playlist not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"playlist": entry,
		"games":    games,
	})
}

func (s *Server) handleAPIViews(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	views, err := db.ListView()
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"views": views,
		"count": len(views),
	})
}

func (s *Server) handleAPIReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		apiError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if !isTailscale(r) {
		apiError(w, http.StatusNotFound, "not found")
		return
	}
	queue, err := db.ListReviewQueue()
	if err != nil {
		apiError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"review_queue": queue,
		"count":        len(queue),
	})
}
