package memory

import (
	"context"
	"math"
	"testing"
)

func TestCosineSimilarityIdentical(t *testing.T) {
	v := []float32{1, 2, 3}
	sim := CosineSimilarity(v, v)
	if diff := math.Abs(float64(sim) - 1.0); diff > 1e-6 {
		t.Fatalf("expected 1.0 for identical vectors, got %f", sim)
	}
}

func TestCosineSimilarityOrthogonal(t *testing.T) {
	a := []float32{1, 0, 0}
	b := []float32{0, 1, 0}
	sim := CosineSimilarity(a, b)
	if diff := math.Abs(float64(sim)); diff > 1e-6 {
		t.Fatalf("expected 0.0 for orthogonal vectors, got %f", sim)
	}
}

func TestCosineSimilarityOpposite(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{-1, 0}
	sim := CosineSimilarity(a, b)
	if diff := math.Abs(float64(sim) + 1.0); diff > 1e-6 {
		t.Fatalf("expected -1.0 for opposite vectors, got %f", sim)
	}
}

func TestCosineSimilarityZeroVector(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{1, 2, 3}
	sim := CosineSimilarity(a, b)
	if sim != 0 {
		t.Fatalf("expected 0 for zero vector, got %f", sim)
	}
}

func TestSerializeDeserializeRoundtrip(t *testing.T) {
	original := []float32{1.5, -2.25, 3.0, 0, math.MaxFloat32}
	blob := SerializeEmbedding(original)

	if len(blob) != len(original)*4 {
		t.Fatalf("expected blob length %d, got %d", len(original)*4, len(blob))
	}

	restored := DeserializeEmbedding(blob)
	if len(restored) != len(original) {
		t.Fatalf("length mismatch: expected %d, got %d", len(original), len(restored))
	}
	for i := range original {
		if original[i] != restored[i] {
			t.Fatalf("value mismatch at index %d: expected %f, got %f", i, original[i], restored[i])
		}
	}
}

func TestSerializeNil(t *testing.T) {
	blob := SerializeEmbedding(nil)
	if blob != nil {
		t.Fatalf("expected nil for nil input, got %v", blob)
	}
	restored := DeserializeEmbedding(nil)
	if restored != nil {
		t.Fatalf("expected nil for nil input, got %v", restored)
	}
}

func TestNoneEmbedder(t *testing.T) {
	e := NoneEmbedder{}
	vecs, err := e.Embed(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vecs != nil {
		t.Fatalf("expected nil, got %v", vecs)
	}
	if e.Dimension() != 0 {
		t.Fatalf("expected dimension 0, got %d", e.Dimension())
	}
}
