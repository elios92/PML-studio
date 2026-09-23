//go:build windows

package main

import "testing"

func TestConnectionsAllowMultipleSameSide(t *testing.T) {
	doc := defaultConnectionDocument()
	doc.Connections = []MapConnection{
		{
			ID: "test-east-1", Order: 0, Enabled: true,
			A: ConnectionEndpoint{MapID: 1, Edge: "EAST", Offset: 0},
			B: ConnectionEndpoint{MapID: 2, Edge: "WEST", Offset: 0},
		},
		{
			ID: "test-east-2", Order: 1, Enabled: true,
			A: ConnectionEndpoint{MapID: 1, Edge: "EAST", Offset: 10},
			B: ConnectionEndpoint{MapID: 3, Edge: "WEST", Offset: 0},
		},
		{
			ID: "test-east-3", Order: 2, Enabled: true,
			A: ConnectionEndpoint{MapID: 1, Edge: "EAST", Offset: 20},
			B: ConnectionEndpoint{MapID: 4, Edge: "WEST", Offset: 0},
		},
	}

	got := effectiveConnectionsForMap(doc, 1)
	if len(got) != 3 {
		t.Fatalf("expected 3 EAST connections, got %d", len(got))
	}
	for i, conn := range got {
		if conn.Direction != "right" {
			t.Fatalf("connection %d: direction=%q, expected right", i+1, conn.Direction)
		}
	}
	if got[0].RecordID != "test-east-1" || got[1].RecordID != "test-east-2" || got[2].RecordID != "test-east-3" {
		t.Fatalf("unexpected connection order: %#v", got)
	}
}

func TestConnectionLegacyClassification(t *testing.T) {
	legacyIDs := []string{"legacy-0090", "legacy-editor-0001", "LEGACY-0002"}
	for _, id := range legacyIDs {
		if !connectionIsLegacyImported(MapConnection{ID: id}) {
			t.Fatalf("%q must be classified as legacy/imported", id)
		}
	}
	if connectionIsLegacyImported(MapConnection{ID: "conn-0001"}) {
		t.Fatal("conn-0001 must not be classified as legacy/imported")
	}
}
