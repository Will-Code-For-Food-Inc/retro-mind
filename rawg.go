package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// RAWGTag is a community-curated label from RAWG (e.g. "atmospheric", "great soundtrack").
type RAWGTag struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// RAWGMeta is the cached result for a single game.
type RAWGMeta struct {
	RAWGID          int       `json:"rawg_id,omitempty"`
	Title           string    `json:"title"`
	Description     string    `json:"description"`
	Tags            []RAWGTag `json:"tags"`
	Released        string    `json:"released"`
	Metacritic      int       `json:"metacritic,omitempty"`
	BackgroundImage string    `json:"background_image,omitempty"`
	FetchedAt       time.Time `json:"fetched_at"`
	NotFound        bool      `json:"not_found,omitempty"`
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

func cacheKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), "-"))
}

// FetchGameMetadata returns RAWG metadata for a game name.
// Returns (meta, cached, error). If the game isn't found on RAWG, meta.NotFound is true.
// Results are cached permanently — each game hits the API at most once.
func FetchGameMetadata(name string) (*RAWGMeta, bool, error) {
	key := cacheKey(name)

	if hit, ok := dbGetCachedMeta(key); ok {
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

	dbPutCachedMeta(key, meta)
	return meta, false, nil
}

func rawgSearch(name, apiKey string) (*RAWGMeta, error) {
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
			ID              int       `json:"id"`
			Name            string    `json:"name"`
			Released        string    `json:"released"`
			Metacritic      int       `json:"metacritic"`
			Tags            []RAWGTag `json:"tags"`
			BackgroundImage string    `json:"background_image"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("RAWG decode: %w", err)
	}
	if len(result.Results) == 0 {
		return &RAWGMeta{Title: name, FetchedAt: time.Now(), NotFound: true}, nil
	}

	top := result.Results[0]
	meta := &RAWGMeta{
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
func FetchGameMetadataByID(gameName string, rawgID int) (*RAWGMeta, error) {
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
		ID              int       `json:"id"`
		Name            string    `json:"name"`
		Released        string    `json:"released"`
		Metacritic      int       `json:"metacritic"`
		Tags            []RAWGTag `json:"tags"`
		BackgroundImage string    `json:"background_image"`
		DescriptionRaw  string    `json:"description_raw"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, fmt.Errorf("RAWG decode: %w", err)
	}
	if detail.ID == 0 {
		return nil, fmt.Errorf("RAWG returned no game for id %d", rawgID)
	}

	meta := &RAWGMeta{
		RAWGID:          detail.ID,
		Title:           detail.Name,
		Released:        detail.Released,
		Metacritic:      detail.Metacritic,
		Tags:            detail.Tags,
		BackgroundImage: detail.BackgroundImage,
		Description:     detail.DescriptionRaw,
		FetchedAt:       time.Now(),
	}

	key := cacheKey(gameName)
	dbPutCachedMeta(key, meta)
	return meta, nil
}
