package mongo

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"strconv"
	"strings"
)

func decodeUserData(data []byte) (map[string]int, error) {
	decompressedData, err := gzipDecompress(data)
	if err != nil {
		return nil, fmt.Errorf("%w: decompress payload: %v", ErrInvalidData, err)
	}

	varList := map[string]int{
		"var100001": 0,
		"var100002": 1,
		"var100003": 2,
		"var100004": 3,
		"var100005": 4,
		"var100006": 5,
	}
	return ProcessUserData(varList, strings.Split(string(decompressedData), ","))
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
