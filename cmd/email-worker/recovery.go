package main

import (
	"context"
	"github.com/City4Christ/City4Christ-PWA-Backend/internal/auth"
)

type guardedRecoverySender struct {
	config workerConfig
	sender auth.RecoveryEmailSender
}

func (s guardedRecoverySender) SendRecovery(ctx context.Context, id string, m auth.RecoveryEmail) error {
	if !s.config.allowed(m.Email) {
		return blockedRecipientError()
	}
	return s.sender.SendRecovery(ctx, id, m)
}
