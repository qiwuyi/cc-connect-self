package memory

// DeleteRequest for deleting memories.
type DeleteRequest struct {
	ID      string         `json:"id,omitempty"`
	Filters map[string]any `json:"filters,omitempty"`
}

// GetAllRequest for retrieving all memories.
type GetAllRequest struct {
	Limit   int            `json:"limit,omitempty"`
	Filters map[string]any `json:"filters,omitempty"`
}

// UsageResponse contains memory usage statistics.
type UsageResponse struct {
	Count        int   `json:"count"`
	TotalBytes   int64 `json:"total_bytes"`
	AvgItemBytes int64 `json:"avg_item_bytes"`
}
