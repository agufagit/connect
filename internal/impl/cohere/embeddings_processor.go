// Copyright 2024 Redpanda Data, Inc.
//
// Licensed as a Redpanda Enterprise file under the Redpanda Community
// License (the "License"); you may not use this file except in compliance with
// the License. You may obtain a copy of the License at
//
// https://github.com/redpanda-data/connect/blob/main/licenses/rcl.md

package cohere

import (
	"context"
	"errors"
	"fmt"

	cohere "github.com/cohere-ai/cohere-go/v2"
	"github.com/redpanda-data/benthos/v4/public/bloblang"
	"github.com/redpanda-data/benthos/v4/public/service"
)

const (
	oepFieldTextMapping = "text_mapping"
	oepFieldInputType   = "dimensions"
)

func init() {
	err := service.RegisterProcessor(
		"cohere_embeddings",
		embeddingProcessorConfig(),
		makeEmbeddingsProcessor,
	)
	if err != nil {
		panic(err)
	}
}

func embeddingProcessorConfig() *service.ConfigSpec {
	return service.NewConfigSpec().
		Categories("AI").
		Summary("Generates vector embeddings to represent input text, using the Cohere API.").
		Description(`
This processor sends text strings to the Cohere API, which generates vector embeddings. By default, the processor submits the entire payload of each message as a string, unless you use the `+"`"+oepFieldTextMapping+"`"+` configuration field to customize it.

To learn more about vector embeddings, see the https://docs.cohere.com/docs/embeddings[Cohere API documentation^].`).
		Version("4.37.0").
		Fields(
			baseConfigFieldsWithModels(
				"embed-english-v3.0",
				"embed-english-light-v3.0",
				"embed-multilingual-v3.0",
				"embed-multilingual-light-v3.0",
			)...,
		).
		Fields(
			service.NewBloblangField(oepFieldTextMapping).
				Description("The text you want to generate a vector embedding for. By default, the processor submits the entire payload as a string.").
				Optional(),
			service.NewStringAnnotatedEnumField(oepFieldInputType, map[string]string{
				"search_document": "Used for embeddings stored in a vector database for search use-cases.",
				"search_query":    "Used for embeddings of search queries run against a vector DB to find relevant documents.",
				"classification":  "Used for embeddings passed through a text classifier.",
				"clustering":      "Used for the embeddings run through a clustering algorithm.",
			}).
				Description("Specifies the type of input passed to the model.").
				Default("search_document"),
		).
		Example(
			"Store embedding vectors in Qdrant",
			"Compute embeddings for some generated data and store it within xrefs:component:outputs/qdrant.adoc[Qdrant]",
			`input:
  generate:
    interval: 1s
    mapping: |
      root = {"text": fake("paragraph")}
pipeline:
  processors:
  - cohere_embeddings:
      model: embed-english-v3
      api_key: "${COHERE_API_KEY}"
      text_mapping: "root = this.text"
output:
  qdrant:
    grpc_host: localhost:6334
    collection_name: "example_collection"
    id: "root = uuid_v4()"
    vector_mapping: "root = this"`)
}

func makeEmbeddingsProcessor(conf *service.ParsedConfig, mgr *service.Resources) (service.Processor, error) {
	b, err := newBaseProcessor(conf)
	if err != nil {
		return nil, err
	}
	var t *bloblang.Executor
	if conf.Contains(oepFieldTextMapping) {
		t, err = conf.FieldBloblang(oepFieldTextMapping)
		if err != nil {
			return nil, err
		}
	}
	var et cohere.EmbedInputType
	if conf.Contains(oepFieldInputType) {
		v, err := conf.FieldString(oepFieldInputType)
		if err != nil {
			return nil, err
		}
		t, err := cohere.NewEmbedInputTypeFromString(v)
		if err != nil {
			return nil, err
		}
		et = t
	}
	return &embeddingsProcessor{b, t, et}, nil
}

type embeddingsProcessor struct {
	*baseProcessor

	text      *bloblang.Executor
	inputType cohere.EmbedInputType
}

func (p *embeddingsProcessor) Process(ctx context.Context, msg *service.Message) (service.MessageBatch, error) {
	var body cohere.EmbedRequest
	body.Model = &p.model
	body.InputType = &p.inputType
	if p.text != nil {
		s, err := msg.BloblangQuery(p.text)
		if err != nil {
			return nil, fmt.Errorf("%s execution error: %w", oepFieldTextMapping, err)
		}
		r, err := s.AsBytes()
		if err != nil {
			return nil, fmt.Errorf("%s extraction error: %w", oepFieldTextMapping, err)
		}
		body.Texts = append(body.Texts, string(r))
	} else {
		b, err := msg.AsBytes()
		if err != nil {
			return nil, err
		}
		body.Texts = append(body.Texts, string(b))
	}
	resp, err := p.client.Embed(ctx, &body)
	if err != nil {
		return nil, err
	}
	if resp.EmbeddingsFloats == nil {
		return nil, errors.New("expected embeddings output")
	}
	if len(resp.EmbeddingsFloats.Embeddings) != 1 {
		return nil, fmt.Errorf("expected a single embeddings response, got: %d", len(resp.EmbeddingsFloats.Embeddings))
	}
	embd := resp.EmbeddingsFloats.Embeddings[0]
	data := make([]any, len(embd))
	for i, f := range embd {
		data[i] = f
	}
	msg = msg.Copy()
	msg.SetStructuredMut(data)
	return service.MessageBatch{msg}, nil
}