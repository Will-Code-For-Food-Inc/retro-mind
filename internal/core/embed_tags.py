#!/usr/bin/env python3
"""Batch-embed text for rom-tagger semantic search.

Reads a JSON array of strings from stdin.
Writes a JSON object mapping text -> embedding vector to stdout.

Model: BAAI/bge-m3 (1024 dimensions, 8192 token context)
Vectors are L2-normalized for cosine similarity via dot product.
"""
import json
import sys


def main():
    try:
        from sentence_transformers import SentenceTransformer
    except ImportError:
        json.dump(
            {"error": "sentence-transformers not installed"},
            sys.stdout,
        )
        sys.exit(1)

    texts = json.loads(sys.stdin.read())
    if not texts:
        json.dump({}, sys.stdout)
        return

    import torch
    device = "cuda" if torch.cuda.is_available() else "cpu"
    model = SentenceTransformer("BAAI/bge-m3", device=device)
    embeddings = model.encode(texts, normalize_embeddings=True, show_progress_bar=False)

    result = {}
    for text, emb in zip(texts, embeddings):
        result[text] = [round(float(x), 6) for x in emb]
    json.dump(result, sys.stdout)


if __name__ == "__main__":
    main()
