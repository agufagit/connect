// Copyright 2024 Redpanda Data, Inc.
//
// Licensed as a Redpanda Enterprise file under the Redpanda Community
// License (the "License"); you may not use this file except in compliance with
// the License. You may obtain a copy of the License at
//
// https://github.com/redpanda-data/redpanda/blob/master/licenses/rcl.md

//go:build !unix

package ollama

import (
	"os/exec"

	"github.com/redpanda-data/connect/v4/internal/singleton"
)

var ollamaProcess *singleton.Singleton[*exec.Cmd]