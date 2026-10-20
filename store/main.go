// Copyright 2026 Matt Harrison
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"store/internal/local"

	spore "github.com/sporeos-dev/spore-client-libs/spore_go"
)

const appId = "dev.sporeos.store"

type app struct {
	cache *local.Cache
}

func main() {

	client := spore.New(appId).WithDefaultErrorHandler()
	err := client.Connect()
	if err != nil {
		panic(err)
	}
	defer client.Disconnect()

	app := &app{
		cache: local.New(client),
	}

	client.OnResponse(app.cache.OnLoad)
	app.cache.Load()

	client.OnRequest(app.cache.Refresh)
	client.OnRequest(app.cache.Browse)
	client.OnRequest(app.cache.Details)
	client.OnRequest(app.cache.Get)

	err = client.Listen()
	if err != nil {
		panic(err)
	}
}

