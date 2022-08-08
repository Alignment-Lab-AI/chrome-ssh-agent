//go:build js && wasm

// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package chrome

import (
	"fmt"
	"strings"
	"syscall/js"
	"testing"

	"github.com/google/chrome-ssh-agent/go/chrome/fakes"
	"github.com/google/chrome-ssh-agent/go/dom"
	"github.com/google/go-cmp/cmp"
	"github.com/norunners/vert"
)

func syncGet(s PersistentStore) (map[string]js.Value, error) {
	datac := make(chan map[string]js.Value, 1)
	errc := make(chan error, 1)
	s.Get(func(data map[string]js.Value, err error) {
		datac <- data
		errc <- err
	})
	return <-datac, <-errc
}

func isManifest(v js.Value) bool {
	var manifest bigValueManifest
	if err := vert.ValueOf(v).AssignTo(&manifest); err == nil && manifest.Valid() {
		return true
	}
	return false
}

func syncGetEntryType(s PersistentStore) (map[string]string, error) {
	data, err := syncGet(s)
	if err != nil {
		return nil, err
	}

	res := map[string]string{}
	for k, v := range data {
		switch {
		case isChunkKey(k):
			res[k] = "chunk"
		case isManifest(v):
			res[k] = "manifest"
		default:
			res[k] = "simple"
		}
	}
	return res, nil
}

func syncGetJSON(s PersistentStore) (map[string]string, error) {
	data, err := syncGet(s)
	if err != nil {
		return nil, err
	}

	json := map[string]string{}
	for k, v := range data {
		json[k] = dom.ToJSON(v)
	}
	return json, nil
}

const (
	defaultMaxItemBytes = 1024
)

func TestSetAndGet(t *testing.T) {
	testcases := []struct {
		description  string
		maxItemBytes int
		set          map[string]js.Value
		wantRaw      map[string]string
		want         map[string]string
	}{
		{
			description: "Simple values of multiple types",
			set: map[string]js.Value{
				"myNumber": js.ValueOf(2),
				"myString": js.ValueOf("foo"),
				"myObject": vert.ValueOf(&myStruct{IntField: 2}).JSValue(),
			},
			wantRaw: map[string]string{
				"myNumber": "simple",
				"myString": "simple",
				"myObject": "simple",
			},
			want: map[string]string{
				"myNumber": "2",
				"myString": `"foo"`,
				"myObject": `{"intField":2,"stringField":""}`,
			},
		},
		{
			description:  "Big values of multiple types",
			maxItemBytes: 200,
			set: map[string]js.Value{
				"myString": js.ValueOf(strings.Repeat("a", 200)),
				"myObject": vert.ValueOf(&myStruct{
					IntField:    2000000,
					StringField: strings.Repeat("a", 200),
				}).JSValue(),
			},
			wantRaw: map[string]string{
				"chunk-3cc36853-b864-4122-beaa-516aa24448f6:BhCOaZDxAkcxzFGDBPBetTErqvNiknYfwvV7xu90ARM=": "chunk",
				"chunk-3cc36853-b864-4122-beaa-516aa24448f6:Fru0sIiU1np0QdrjNzVcQQnL4/go9+Bhsa0jum0KFbU=": "chunk",
				"chunk-3cc36853-b864-4122-beaa-516aa24448f6:G6T7G7fdARNR9OSgrLFctjhsP2mKdz4GS9bvK8F21ek=": "chunk",
				"chunk-3cc36853-b864-4122-beaa-516aa24448f6:Q1/qr0+WtjHWwzblCloPdGhtv2Ovcx5jlmZcW/XJH0E=": "chunk",
				"chunk-3cc36853-b864-4122-beaa-516aa24448f6:lHZRIv7UAumQRGrzQCQplvRz6iS71g6jnTlZwEhQQcs=": "chunk",
				"myObject": "manifest",
				"myString": "manifest",
			},
			want: map[string]string{
				"myString": fmt.Sprintf(`"%s"`, strings.Repeat("a", 200)),
				"myObject": fmt.Sprintf(`{"intField":2000000,"stringField":"%s"}`, strings.Repeat("a", 200)),
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.description, func(t *testing.T) {
			if tc.maxItemBytes == 0 {
				tc.maxItemBytes = defaultMaxItemBytes
			}

			b := &BigStorage{
				maxItemBytes: tc.maxItemBytes,
				s:            fakes.NewMemStorage(),
			}

			b.Set(tc.set, func(err error) {
				if err != nil {
					t.Fatalf("set failed: %v", err)
				}

				gotRaw, err := syncGetEntryType(b.s)
				if err != nil {
					t.Fatalf("get failed for underlying storage: %v", err)
				}
				got, err := syncGetJSON(b)
				if err != nil {
					t.Fatalf("get failed for BigStorage: %v", err)
				}

				if diff := cmp.Diff(gotRaw, tc.wantRaw); diff != "" {
					t.Errorf("incorrect raw data: -got +want: %s", diff)
				}
				if diff := cmp.Diff(got, tc.want); diff != "" {
					t.Errorf("incorrect data: -got +want: %s", diff)
				}
			})
		})
	}
}

func TestDelete(t *testing.T) {
	testcases := []struct {
		description  string
		maxItemBytes int
		set          map[string]js.Value
		del          []string
		wantRaw      map[string]string
		want         map[string]string
	}{
		{
			description: "Delete simple values",
			set: map[string]js.Value{
				"myNumber": js.ValueOf(2),
				"myString": js.ValueOf("foo"),
				"myObject": vert.ValueOf(&myStruct{IntField: 2}).JSValue(),
			},
			del: []string{
				"myNumber",
				"myObject",
			},
			wantRaw: map[string]string{
				"myString": "simple",
			},
			want: map[string]string{
				"myString": `"foo"`,
			},
		},
		{
			description:  "Delete big values",
			maxItemBytes: 200,
			set: map[string]js.Value{
				"myString": js.ValueOf(strings.Repeat("a", 200)),
				"myObject": vert.ValueOf(&myStruct{
					IntField:    2000000,
					StringField: strings.Repeat("a", 200),
				}).JSValue(),
			},
			del: []string{
				"myObject",
			},
			wantRaw: map[string]string{
				"chunk-3cc36853-b864-4122-beaa-516aa24448f6:Fru0sIiU1np0QdrjNzVcQQnL4/go9+Bhsa0jum0KFbU=": "chunk",
				"chunk-3cc36853-b864-4122-beaa-516aa24448f6:G6T7G7fdARNR9OSgrLFctjhsP2mKdz4GS9bvK8F21ek=": "chunk",
				"chunk-3cc36853-b864-4122-beaa-516aa24448f6:lHZRIv7UAumQRGrzQCQplvRz6iS71g6jnTlZwEhQQcs=": "chunk",
				"myString": "manifest",
			},
			want: map[string]string{
				"myString": fmt.Sprintf(`"%s"`, strings.Repeat("a", 200)),
			},
		},
		{
			description:  "Delete big values that reference same data chunk",
			maxItemBytes: 200,
			set: map[string]js.Value{
				"myString":   js.ValueOf(strings.Repeat("a", 200)),
				"yourString": js.ValueOf(strings.Repeat("a", 200)),
			},
			del: []string{
				"yourString",
			},
			wantRaw: map[string]string{
				"chunk-3cc36853-b864-4122-beaa-516aa24448f6:Fru0sIiU1np0QdrjNzVcQQnL4/go9+Bhsa0jum0KFbU=": "chunk",
				"chunk-3cc36853-b864-4122-beaa-516aa24448f6:G6T7G7fdARNR9OSgrLFctjhsP2mKdz4GS9bvK8F21ek=": "chunk",
				"chunk-3cc36853-b864-4122-beaa-516aa24448f6:lHZRIv7UAumQRGrzQCQplvRz6iS71g6jnTlZwEhQQcs=": "chunk",
				"myString": "manifest",
			},
			want: map[string]string{
				"myString": fmt.Sprintf(`"%s"`, strings.Repeat("a", 200)),
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.description, func(t *testing.T) {
			if tc.maxItemBytes == 0 {
				tc.maxItemBytes = defaultMaxItemBytes
			}

			b := &BigStorage{
				maxItemBytes: tc.maxItemBytes,
				s:            fakes.NewMemStorage(),
			}

			b.Set(tc.set, func(err error) {
				if err != nil {
					t.Fatalf("set failed: %v", err)
				}

				b.Delete(tc.del, func(err error) {
					if err != nil {
						t.Fatalf("delete failed: %v", err)
					}

					gotRaw, err := syncGetEntryType(b.s)
					if err != nil {
						t.Fatalf("get failed for underlying storage: %v", err)
					}
					got, err := syncGetJSON(b)
					if err != nil {
						t.Fatalf("get failed for BigStorage: %v", err)
					}

					if diff := cmp.Diff(gotRaw, tc.wantRaw); diff != "" {
						t.Errorf("incorrect raw data: -got +want: %s", diff)
					}
					if diff := cmp.Diff(got, tc.want); diff != "" {
						t.Errorf("incorrect data: -got +want: %s", diff)
					}
				})
			})
		})
	}
}