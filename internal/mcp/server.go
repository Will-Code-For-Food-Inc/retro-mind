package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/agent"
)

// Server holds shared state for the MCP tool server.
type Server struct {
	RomBase string
	Agent   *agent.Agent
}

// NewServer creates a Server with the given ROM base path and optional agent.
func NewServer(romBase string, ag *agent.Agent) *Server {
	return &Server{RomBase: romBase, Agent: ag}
}

// Handle dispatches a JSON-RPC request.
func (s *Server) Handle(req Request, write WriteFn) {
	switch req.Method {
	case "initialize":
		WriteResult(req.ID, map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{"tools": map[string]interface{}{}},
			"serverInfo":      map[string]string{"name": "rom-tagger", "version": "2.0.0"},
		}, write)
	case "notifications/initialized":
		// no response for notifications
	case "tools/list":
		WriteResult(req.ID, map[string]interface{}{"tools": s.ToolSchemas()}, write)
	case "tools/call":
		var p struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			WriteError(req.ID, -32602, "invalid params", nil, write)
			return
		}
		result, isError := s.CallTool(p.Name, p.Arguments)
		WriteResult(req.ID, map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": result},
			},
			"isError": isError,
		}, write)
	default:
		WriteError(req.ID, -32601, "method not found", nil, write)
	}
}

// ToolsJSON returns the tool schemas as a JSON string (for agent prompt injection).
func (s *Server) ToolsJSON() string {
	data, _ := json.Marshal(s.ToolSchemas())
	return string(data)
}

// CallTool dispatches a tool call by name and returns (result, isError).
func (s *Server) CallTool(name string, args map[string]interface{}) (string, bool) {
	switch name {
	case "list_tags":
		return s.toolListTags()
	case "check_tag":
		tag, _ := args["tag"].(string)
		return s.toolCheckTag(tag)
	case "add_tag":
		tag, _ := args["tag"].(string)
		return s.toolAddTag(tag)
	case "set_game_tags":
		gameName, _ := args["name"].(string)
		platform, _ := args["platform"].(string)
		tags := toStrSlice(args["tags"])
		crcs := toStrSlice(args["crcs"])
		return s.toolSetGameTags(gameName, platform, tags, crcs)
	case "get_game":
		n, _ := args["name"].(string)
		return s.toolGetGame(n)
	case "get_game_by_crc":
		crc, _ := args["crc"].(string)
		return s.toolGetGameByCRC(crc)
	case "list_games":
		platform, _ := args["platform"].(string)
		return s.toolListGames(platform)
	case "clean_rom_name":
		filename, _ := args["filename"].(string)
		return s.toolCleanROMName(filename)
	case "fetch_game_metadata":
		name, _ := args["name"].(string)
		return s.toolFetchGameMetadata(name)
	case "fetch_game_metadata_by_id":
		name, _ := args["name"].(string)
		rawgID := 0
		if v, ok := args["rawg_id"].(float64); ok {
			rawgID = int(v)
		}
		return s.toolFetchGameMetadataByID(name, rawgID)
	case "get_work_batch":
		platform, _ := args["platform"].(string)
		batchSize := 8
		if v, ok := args["batch_size"].(float64); ok && v > 0 {
			batchSize = int(v)
		}
		return s.toolGetWorkBatch(platform, batchSize)
	case "prime_metadata_cache":
		platform, _ := args["platform"].(string)
		return s.toolPrimeMetadataCache(platform)
	case "resync_metadata_cache":
		return s.toolResyncMetadataCache()
	case "reset_queue":
		platform, _ := args["platform"].(string)
		return s.toolResetQueue(platform)
	case "create_playlist":
		n, _ := args["name"].(string)
		desc, _ := args["description"].(string)
		gameNames := toStrSlice(args["games"])
		return s.toolCreatePlaylist(n, desc, gameNames)
	case "get_playlist":
		n, _ := args["name"].(string)
		return s.toolGetPlaylist(n)
	case "list_playlists":
		return s.toolListPlaylists()
	case "delete_playlist":
		n, _ := args["name"].(string)
		return s.toolDeletePlaylist(n)
	case "merge_tags":
		keep, _ := args["keep"].(string)
		merge := toStrSlice(args["merge"])
		return s.toolMergeTags(keep, merge)
	case "delete_tag":
		tag, _ := args["tag"].(string)
		return s.toolDeleteTag(tag)
	case "sync_embeddings":
		return s.toolSyncEmbeddings()
	case "sync_game_embeddings":
		return s.toolSyncGameEmbeddings()
	case "similar_games":
		n, _ := args["name"].(string)
		tags := toStrSlice(args["tags"])
		limit := 10
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}
		return s.toolSimilarGames(n, tags, limit)
	case "game_vibes":
		n, _ := args["name"].(string)
		limit := 10
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}
		return s.toolGameVibes(n, limit)
	case "tag_clusters":
		threshold := float32(0.75)
		if v, ok := args["threshold"].(float64); ok && v > 0 {
			threshold = float32(v)
		}
		return s.toolTagClusters(threshold)
	case "emit_playlist":
		n, _ := args["name"].(string)
		format, _ := args["format"].(string)
		outDir, _ := args["out_dir"].(string)
		return s.toolEmitPlaylist(n, format, outDir)
	case "flag_for_review":
		gameName, _ := args["game_name"].(string)
		notes, _ := args["notes"].(string)
		return s.toolFlagForReview(gameName, notes)
	case "agent_query":
		return s.callAgentTool(args)
	default:
		return fmt.Sprintf(`{"error":"unknown tool: %s"}`, name), true
	}
}
