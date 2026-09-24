package db_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/dbtest"
	"github.com/Asion001/mangarr/internal/model"
)

func TestMessengerDeliveryPersistence(t *testing.T) {
	dbtest.ForEachDialect(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		now := time.Now().UTC().Truncate(time.Millisecond)

		group := &model.Group{
			Name:        "Readers",
			Permissions: []string{"series.view"},
			IncludeTags: []int64{},
			ExcludeTags: []int64{},
			RootFolders: []int64{},
			CreatedAt:   now,
		}
		if _, err := d.NewInsert().Model(group).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		user := &model.User{Username: "reader", GroupID: group.ID, CreatedAt: now}
		if _, err := d.NewInsert().Model(user).Exec(ctx); err != nil {
			t.Fatal(err)
		}

		telegram := &model.MessengerLink{
			UserID: user.ID, Kind: model.MessengerTelegram, ExternalID: "tg-42",
			DisplayName: "Reader on Telegram", Mode: model.DeliveryInstant,
			Events: []string{"chapter.downloaded"}, Status: model.LinkActive,
			CreatedAt: now, UpdatedAt: now,
		}
		discord := &model.MessengerLink{
			UserID: user.ID, Kind: model.MessengerDiscord, ExternalID: "dc-42",
			DisplayName: "Reader on Discord", Mode: model.DeliveryDigest,
			Events: []string{"chapter.downloaded", "request.updated"}, Status: model.LinkActive,
			CreatedAt: now, UpdatedAt: now,
		}
		for _, link := range []*model.MessengerLink{telegram, discord} {
			if _, err := d.NewInsert().Model(link).Exec(ctx); err != nil {
				t.Fatal(err)
			}
		}

		token := &model.MessengerLinkToken{
			TokenHash: "sha256:link-token", UserID: user.ID, Kind: model.MessengerTelegram,
			ExpiresAt: now.Add(15 * time.Minute), CreatedAt: now,
		}
		if _, err := d.NewInsert().Model(token).Exec(ctx); err != nil {
			t.Fatal(err)
		}

		delivery := &model.NotificationDelivery{
			UserID: user.ID, DedupeKey: "chapter:123:user:42", EventType: "chapter.downloaded",
			Payload: map[string]any{"seriesTitle": "Dungeon Meshi", "chapter": "14"}, CreatedAt: now,
		}
		if _, err := d.NewInsert().Model(delivery).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		for _, linkID := range []int64{telegram.ID, discord.ID} {
			dispatch := &model.NotificationDispatch{
				DeliveryID: delivery.ID, LinkID: linkID, AvailableAt: now,
			}
			if _, err := d.NewInsert().Model(dispatch).Exec(ctx); err != nil {
				t.Fatal(err)
			}
		}

		var gotLink model.MessengerLink
		if err := d.NewSelect().Model(&gotLink).Where("id = ?", discord.ID).Scan(ctx); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(gotLink.Events, discord.Events) {
			t.Fatalf("events round trip: got %v want %v", gotLink.Events, discord.Events)
		}
		var gotDelivery model.NotificationDelivery
		if err := d.NewSelect().Model(&gotDelivery).Where("id = ?", delivery.ID).Scan(ctx); err != nil {
			t.Fatal(err)
		}
		if gotDelivery.Payload["seriesTitle"] != "Dungeon Meshi" || gotDelivery.Payload["chapter"] != "14" {
			t.Fatalf("payload round trip: %#v", gotDelivery.Payload)
		}

		duplicate := &model.NotificationDelivery{
			UserID: user.ID, DedupeKey: delivery.DedupeKey, EventType: delivery.EventType,
			Payload: map[string]any{}, CreatedAt: now,
		}
		if _, err := d.NewInsert().Model(duplicate).Exec(ctx); err == nil {
			t.Fatal("expected duplicate user/dedupe key to fail")
		}

		sentAt := now.Add(time.Second)
		if _, err := d.NewUpdate().Model((*model.NotificationDispatch)(nil)).
			Set("sent_at = ?", sentAt).Set("attempts = attempts + 1").
			Where("delivery_id = ? AND link_id = ?", delivery.ID, telegram.ID).Exec(ctx); err != nil {
			t.Fatal(err)
		}
		var dispatches []model.NotificationDispatch
		if err := d.NewSelect().Model(&dispatches).Where("delivery_id = ?", delivery.ID).
			OrderExpr("link_id ASC").Scan(ctx); err != nil {
			t.Fatal(err)
		}
		if len(dispatches) != 2 || dispatches[0].SentAt == nil || dispatches[1].SentAt != nil {
			t.Fatalf("dispatch state was not independent: %#v", dispatches)
		}

		if _, err := d.NewDelete().Model(user).WherePK().Exec(ctx); err != nil {
			t.Fatal(err)
		}
		for name, modelValue := range map[string]any{
			"links":      (*model.MessengerLink)(nil),
			"tokens":     (*model.MessengerLinkToken)(nil),
			"deliveries": (*model.NotificationDelivery)(nil),
			"dispatches": (*model.NotificationDispatch)(nil),
		} {
			count, err := d.NewSelect().Model(modelValue).Count(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("%s did not cascade: %d rows remain", name, count)
			}
		}
	})
}
