package rawg

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/Will-Code-For-Food-Inc/retro-mind/internal/db"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

// FetchGameMetadata returns RAWG metadata for a game name.
// Returns (meta, cached, error). If the game isn't found on RAWG, meta.NotFound is true.
// Results are cached permanently — each game hits the API at most once.
func FetchGameMetadata(name string) (*db.RAWGMeta, bool, error) {
	key := db.CacheKey(name)

	if hit, ok := db.GetCachedMeta(key); ok {
		return hit, true, nil
	}

	apiKey := os.Getenv("RAWG_API_KEY")
	if apiKey == "" {
		return nil, false, fmt.Errorf("RAWG_API_KEY not set")
	}

	meta, err := rawgSearch(name, apiKey)
	if err != nil {
		return nil, false, err
	}

	db.PutCachedMeta(key, meta)
	return meta, false, nil
}

func rawgSearch(name, apiKey string) (*db.RAWGMeta, error) {
	searchURL := "https://api.rawg.io/api/games?key=" + apiKey +
		"&search=" + url.QueryEscape(name) +
		"&page_size=5"

	resp, err := httpClient.Get(searchURL)
	if err != nil {
		return nil, fmt.Errorf("RAWG search: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Results []struct {
			ID              int        `json:"id"`
			Name            string     `json:"name"`
			Released        string     `json:"released"`
			Metacritic      int        `json:"metacritic"`
			Tags            []db.RAWGTag `json:"tags"`
			BackgroundImage string     `json:"background_image"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("RAWG decode: %w", err)
	}
	if len(result.Results) == 0 {
		return &db.RAWGMeta{Title: name, FetchedAt: time.Now(), NotFound: true}, nil
	}

	top := result.Results[0]
	meta := &db.RAWGMeta{
		RAWGID:          top.ID,
		Title:           top.Name,
		Released:        top.Released,
		Metacritic:      top.Metacritic,
		Tags:            top.Tags,
		BackgroundImage: top.BackgroundImage,
		FetchedAt:       time.Now(),
	}

	detailURL := fmt.Sprintf("https://api.rawg.io/api/games/%d?key=%s", top.ID, apiKey)
	if dresp, err := httpClient.Get(detailURL); err == nil {
		defer dresp.Body.Close()
		var detail struct {
			DescriptionRaw string `json:"description_raw"`
		}
		if json.NewDecoder(dresp.Body).Decode(&detail) == nil {
			meta.Description = detail.DescriptionRaw
		}
	}

	return meta, nil
}

// FetchGameMetadataByID fetches RAWG metadata for a known RAWG game ID,
// bypassing name-based search entirely. Useful when the correct ID is known
// from external research (e.g. RAWG URL, OpenVGDB lookup).
// Results are stored in rawg_cache under the game name slug like normal.
func FetchGameMetadataByID(gameName string, rawgID int) (*db.RAWGMeta, error) {
	apiKey := os.Getenv("RAWG_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("RAWG_API_KEY not set")
	}

	detailURL := fmt.Sprintf("https://api.rawg.io/api/games/%d?key=%s", rawgID, apiKey)
	resp, err := httpClient.Get(detailURL)
	if err != nil {
		return nil, fmt.Errorf("RAWG fetch by id: %w", err)
	}
	defer resp.Body.Close()

	var detail struct {
		ID              int        `json:"id"`
		Name            string     `json:"name"`
		Released        string     `json:"released"`
		Metacritic      int        `json:"metacritic"`
		Tags            []db.RAWGTag `json:"tags"`
		BackgroundImage string     `json:"background_image"`
		DescriptionRaw  string     `json:"description_raw"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, fmt.Errorf("RAWG decode: %w", err)
	}
	if detail.ID == 0 {
		return nil, fmt.Errorf("RAWG returned no game for id %d", rawgID)
	}

	meta := &db.RAWGMeta{
		RAWGID:          detail.ID,
		Title:           detail.Name,
		Released:        detail.Released,
		Metacritic:      detail.Metacritic,
		Tags:            detail.Tags,
		BackgroundImage: detail.BackgroundImage,
		Description:     detail.DescriptionRaw,
		FetchedAt:       time.Now(),
	}

	key := db.CacheKey(gameName)
	db.PutCachedMeta(key, meta)
	return meta, nil
}
