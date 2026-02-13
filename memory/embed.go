package memory

import (
	"context"
	"encoding/binary"
	"math"
)

// Embedder generates vector embeddings for text.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
}

// NoneEmbedder is a no-op embedder that disables vector search.
type NoneEmbedder struct{}

func (NoneEmbedder) Embed(context.Context, []string) ([][]float32, error) { return nil, nil }
func (NoneEmbedder) Dimension() int                                       { return 0 }

// CosineSimilarity returns the cosine similarity between two vectors.
// Returns 0 if either vector has zero magnitude.
func CosineSimilarity(a, b []float32) float32 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return float32(dot / denom)
}

// SerializeEmbedding converts a float32 slice to a little-endian byte slice
// suitable for storage in a BLOB column.
func SerializeEmbedding(v []float32) []byte {
	if v == nil {
		return nil
	}
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// DeserializeEmbedding converts a little-endian BLOB back to a float32 slice.
func DeserializeEmbedding(blob []byte) []float32 {
	if blob == nil {
		return nil
	}
	n := len(blob) / 4
	v := make([]float32, n)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
	}
	return v
}
