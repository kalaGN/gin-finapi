package mongo

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/kalaGN/gin-finapi/pkg/utils"
)

func decodeUserData(ctx context.Context, data []byte, varList map[string]int) (map[string]int, error) {
	if len(varList) == 0 {
		return nil, fmt.Errorf("%w: mapping is required", ErrInvalidData)
	}
	decompressedData, err := gzipDecompress(data)
	utils.MarkTiming(ctx, "gzip")
	if err != nil {
		return nil, fmt.Errorf("%w: decompress payload: %v", ErrInvalidData, err)
	}
	result, err := ProcessUserData(varList, strings.Split(string(decompressedData), ","))
	utils.MarkTiming(ctx, "csv_map")
	return result, err
}

func gzipDecompress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty input data")
	}

	buf := bytes.NewBuffer(data)
	gz, err := gzip.NewReader(buf)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	var out bytes.Buffer
	_, err = out.ReadFrom(gz)
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func ProcessUserData(varlist map[string]int, userDataArray []string) (map[string]int, error) {
	requiredFields := 0
	for _, index := range varlist {
		if index < 0 {
			return nil, fmt.Errorf("%w: negative field index %d", ErrInvalidData, index)
		}
		if index+1 > requiredFields {
			requiredFields = index + 1
		}
	}
	if len(userDataArray) != requiredFields {
		return nil, fmt.Errorf("%w: got %d fields, want %d", ErrInvalidData, len(userDataArray), requiredFields)
	}

	result := make(map[string]int)

	for key, index := range varlist {
		value, err := strconv.Atoi(userDataArray[index])
		if err != nil {
			return nil, fmt.Errorf("%w: field %s is not an integer", ErrInvalidData, key)
		}
		result[key] = value
	}

	return result, nil
}
