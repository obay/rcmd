package main

import "context"

type ctxKey int

const ctxKeyBody ctxKey = 1

func withBody(ctx context.Context, body []byte) context.Context {
	return context.WithValue(ctx, ctxKeyBody, body)
}

func bodyFrom(ctx context.Context) []byte {
	b, _ := ctx.Value(ctxKeyBody).([]byte)
	return b
}
