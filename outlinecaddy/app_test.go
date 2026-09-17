// Copyright 2026 The Outline Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package outlinecaddy

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	countryFixture = "../third_party/maxmind/test-data/GeoLite2-Country-Test.mmdb"
	asnFixture     = "../third_party/maxmind/test-data/GeoLite2-ASN-Test.mmdb"
)

func provisionApp(t *testing.T, cfg *IPInfoConfig) *OutlineApp {
	t.Helper()

	ctx, cancel := caddy.NewContext(caddy.Context{Context: context.Background()})
	t.Cleanup(cancel)

	app := &OutlineApp{IPInfo: cfg}

	require.NoError(t, app.Provision(ctx))

	t.Cleanup(func() { _ = app.Cleanup() })

	return app
}

func TestIPInfoDisabledByDefault(t *testing.T) {
	app := provisionApp(t, nil)

	assert.Nil(t, app.ipInfo)
	assert.Nil(t, app.ipInfoKey)

	require.NoError(t, app.Cleanup())
}

func TestIPInfoLookup(t *testing.T) {
	app := provisionApp(t, &IPInfoConfig{
		CountryDB: countryFixture,
		ASNDB:     asnFixture,
	})

	require.NotNil(t, app.ipInfo, "ipInfo should be initialized")

	ipInfo, err := app.ipInfo.GetIPInfo(net.ParseIP("111.235.160.0"))
	require.NoError(t, err)
	assert.Equal(t, "CN", ipInfo.CountryCode.String())

	ipInfo, err = app.ipInfo.GetIPInfo(net.ParseIP("38.108.80.24"))
	require.NoError(t, err)
	assert.Equal(t, 174, ipInfo.ASN.Number)
}

func TestIPInfoSharedAcrossReloads(t *testing.T) {
	firstApp := provisionApp(t, &IPInfoConfig{
		CountryDB: countryFixture,
		ASNDB:     asnFixture,
	})
	secondApp := provisionApp(t, &IPInfoConfig{
		CountryDB: countryFixture,
		ASNDB:     asnFixture,
	})

	assert.Equal(t, *firstApp.ipInfoKey, *secondApp.ipInfoKey)

	// 2 references expected.
	refs, _ := ipInfoPool.References(*firstApp.ipInfoKey)
	assert.Equal(t, 2, refs)

	// Cleanup the first app.
	key := firstApp.ipInfoKey
	require.NoError(t, firstApp.Cleanup())

	// 1 reference expected, same key (the second app).
	refs, _ = ipInfoPool.References(*key)
	assert.Equal(t, 1, refs)

	secondAppKey := secondApp.ipInfoKey
	require.NoError(t, secondApp.Cleanup())

	_, exist := ipInfoPool.References(*secondAppKey)
	assert.False(t, exist)
}

func TestIPInfoReopensOnChange(t *testing.T) {
	tmpDir := t.TempDir()
	tmpPath := filepath.Join(tmpDir, "ip-country.mmdb")
	tmpData, err := os.ReadFile(countryFixture)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(tmpPath, tmpData, 0o644))

	firstApp := provisionApp(t, &IPInfoConfig{
		CountryDB: tmpPath,
	})

	after := time.Now().Add(time.Hour)
	require.NoError(t, os.Chtimes(tmpPath, after, after))

	secondApp := provisionApp(t, &IPInfoConfig{
		CountryDB: tmpPath,
	})

	assert.NotEqual(t, *firstApp.ipInfoKey, *secondApp.ipInfoKey)

	refs, _ := ipInfoPool.References(*firstApp.ipInfoKey)
	assert.Equal(t, 1, refs)

	refs, _ = ipInfoPool.References(*secondApp.ipInfoKey)
	assert.Equal(t, 1, refs)

	firstAppKey := firstApp.ipInfoKey
	secondAppKey := secondApp.ipInfoKey
	require.NoError(t, firstApp.Cleanup())
	require.NoError(t, secondApp.Cleanup())

	_, exist := ipInfoPool.References(*firstAppKey)
	assert.False(t, exist)

	_, exist = ipInfoPool.References(*secondAppKey)
	assert.False(t, exist)
}

func TestIPInfoInvalidConfigNoProvisionError(t *testing.T) {
	tmpDir := t.TempDir()
	tmpPath := filepath.Join(tmpDir, "noop.mmdb")

	app := provisionApp(t, &IPInfoConfig{
		CountryDB: tmpPath,
	})

	assert.Nil(t, app.ipInfo)
	assert.Nil(t, app.ipInfoKey)
}

func TestIPInfoPartialConfig(t *testing.T) {
	tmpDir := t.TempDir()
	tmpPath := filepath.Join(tmpDir, "noop.mmdb")

	app := provisionApp(t, &IPInfoConfig{
		CountryDB: tmpPath,
		ASNDB:     asnFixture,
	})

	require.NotNil(t, app.ipInfo)
	require.NotNil(t, app.ipInfoKey)

	assert.Equal(t, "", app.ipInfoKey.countryDBPath)
	assert.Equal(t, asnFixture, app.ipInfoKey.asnDBPath)

	ipInfo, err := app.ipInfo.GetIPInfo(net.ParseIP("38.108.80.24"))
	require.NoError(t, err)
	assert.Equal(t, 174, ipInfo.ASN.Number)
	assert.Equal(t, "", ipInfo.CountryCode.String())
}
