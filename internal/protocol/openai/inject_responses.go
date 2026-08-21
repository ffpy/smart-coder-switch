package openai

import (
	"encoding/json"
	"fmt"
)

// ResponsesAppendUserInputItem 在 Responses input 数组末尾追加一条 user input item。
func ResponsesAppendUserInputItem(
	rawBody []byte,
	content string,
) ([]byte, error) {
	var request map[string]json.RawMessage

	if err := json.Unmarshal(rawBody, &request); err != nil {
		return nil, fmt.Errorf(
			"parse request for append responses input item: %w",
			err,
		)
	}

	rawInput, ok := request["input"]
	if !ok {
		return nil, fmt.Errorf(
			"input is required for append responses input item",
		)
	}

	inputItems, err := responsesParseInputItems(rawInput)
	if err != nil {
		return nil, err
	}

	userItem, err := json.Marshal(map[string]string{
		"role":    "user",
		"content": content,
	})
	if err != nil {
		return nil, fmt.Errorf("encode responses user input item: %w", err)
	}

	inputItems = append(inputItems, userItem)

	encodedInput, err := json.Marshal(inputItems)
	if err != nil {
		return nil, fmt.Errorf("encode responses input: %w", err)
	}

	request["input"] = encodedInput

	result, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode responses request: %w", err)
	}

	return result, nil
}

func responsesParseInputItems(raw json.RawMessage) ([]json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("input is empty")
	}

	if !json.Valid(raw) {
		return nil, fmt.Errorf("input is not valid json")
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		item, err := json.Marshal(map[string]string{
			"type": "text",
			"text": text,
		})
		if err != nil {
			return nil, fmt.Errorf("encode responses input text item: %w", err)
		}
		return []json.RawMessage{item}, nil
	}

	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("parse responses input items: %w", err)
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("responses input items must not be empty")
	}

	return items, nil
}
