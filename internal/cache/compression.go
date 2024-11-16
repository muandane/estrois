package cache

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"
)

// ShouldCompress determines if content should be compressed based on type and size
func ShouldCompress(contentType string, size int64) bool {
	compressibleTypes := map[string]bool{
		"text/":                  true,
		"application/json":       true,
		"application/javascript": true,
		"application/xml":        true,
		"application/yaml":       true,
		"image/svg":              true,
	}

	if size < MinSizeForCompression {
		return false
	}

	for t := range compressibleTypes {
		if strings.HasPrefix(contentType, t) {
			return true
		}
	}
	return false
}

// CompressData compresses byte data using gzip
func CompressData(data []byte) ([]byte, error) {
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)

	if _, err := gzipWriter.Write(data); err != nil {
		return nil, err
	}

	if err := gzipWriter.Close(); err != nil {
		return nil, err
	}

	return compressed.Bytes(), nil
}

// DecompressData decompresses gzipped byte data
func DecompressData(data []byte) ([]byte, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()

	return io.ReadAll(gzipReader)
}
