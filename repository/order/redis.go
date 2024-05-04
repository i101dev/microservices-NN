package order

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/i101dev/microservices-NN/model"
	"github.com/redis/go-redis/v9"
)

var ErrNotExist = errors.New("order does not exist")

type RedisRepo struct {
	Client *redis.Client
}
type FindAllPage struct {
	Size   uint
	Offset uint
}

type FindResult struct {
	Orders []model.Order
	Cursor uint64
}

func orderIDKey(id uint64) string {
	return fmt.Sprintf("order:%d", id)
}

func (r *RedisRepo) Insert(ctx context.Context, order model.Order) error {

	data, err := json.Marshal(order)

	if err != nil {
		return fmt.Errorf("failed to encode order to JSON: %w", err)
	}

	key := orderIDKey(uint64(order.OrderID))
	txn := r.Client.TxPipeline()

	res := txn.SetNX(ctx, key, string(data), 0)
	if err := res.Err(); err != nil {
		txn.Discard()
		return fmt.Errorf("failed to set: %w", err)
	}

	if err := txn.SAdd(ctx, "orders", key).Err(); err != nil {
		txn.Discard()
		return fmt.Errorf("failed to add orders to set: %w", err)
	}

	if _, err := txn.Exec(ctx); err != nil {
		return fmt.Errorf("failed to execute [insert] transaction: %w", err)
	}

	return nil
}

func (r *RedisRepo) FindByID(ctx context.Context, id uint64) (model.Order, error) {

	key := orderIDKey(id)

	value, err := r.Client.Get(ctx, key).Result()

	if errors.Is(err, redis.Nil) {
		return model.Order{}, ErrNotExist
	} else if err != nil {
		return model.Order{}, fmt.Errorf("error getting order: %w", err)
	}

	var order model.Order

	if err = json.Unmarshal([]byte(value), &order); err != nil {
		return model.Order{}, fmt.Errorf("failed to decode order to JSON: %w", err)
	}

	return order, nil
}

func (r *RedisRepo) DeleteByID(ctx context.Context, id uint64) error {

	key := orderIDKey(id)

	txn := r.Client.TxPipeline()
	err := txn.Del(ctx, key).Err()

	if errors.Is(err, redis.Nil) {
		txn.Discard()
		return ErrNotExist
	} else if err != nil {
		txn.Discard()
		return fmt.Errorf("error getting order: %w", err)
	}

	if err := txn.SRem(ctx, "orders", key).Err(); err != nil {
		txn.Discard()
		return fmt.Errorf("failed to remove from orders set: %w", err)
	}

	if _, err := txn.Exec(ctx); err != nil {
		return fmt.Errorf("failed to execute [delete] transaction: %w", err)
	}

	return nil
}

func (r *RedisRepo) Update(ctx context.Context, order model.Order) error {

	data, err := json.Marshal(order)

	if err != nil {
		return fmt.Errorf("failed to encode order to JSON: %w", err)
	}

	key := orderIDKey(uint64(order.OrderID))

	err = r.Client.SetXX(ctx, key, string(data), 0).Err()

	if errors.Is(err, redis.Nil) {
		return ErrNotExist
	} else if err != nil {
		return fmt.Errorf("error getting order: %w", err)
	}

	return nil
}

func (r *RedisRepo) FindAll(ctx context.Context, page FindAllPage) (FindResult, error) {

	res := r.Client.SScan(ctx, "orders", uint64(page.Offset), "*", int64(page.Size))

	keys, cursor, err := res.Result()

	if len(keys) == 0 {
		return FindResult{
			Orders: []model.Order{},
		}, nil
	}

	if err != nil {
		return FindResult{}, fmt.Errorf("failed to get order IDs: %w", err)
	}

	xs, err := r.Client.MGet(ctx, keys...).Result()

	if err != nil {
		return FindResult{}, fmt.Errorf("failed to [MGet] orders: %w", err)
	}

	orders := make([]model.Order, len(xs))

	for i, x := range xs {
		x := x.(string)

		var order model.Order
		if err := json.Unmarshal([]byte(x), &order); err != nil {
			return FindResult{}, fmt.Errorf("failed to decode order to JSON: %w", err)
		}

		orders[i] = order
	}

	return FindResult{
		Orders: orders,
		Cursor: cursor,
	}, nil
}
