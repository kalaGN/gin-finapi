package repositories

import (
	"context"

	"github.com/kalaGN/gin-finapi/app/models"
)

// LoanRepository 贷款数据仓库接口
type LoanRepository interface {
	Create(ctx context.Context, loan *models.LoanRequest) error
	FindByID(ctx context.Context, id string) (*models.LoanRequest, error)
	FindBySessionID(ctx context.Context, sid string) (*models.LoanRequest, error)
	Update(ctx context.Context, loan *models.LoanRequest) error
	Delete(ctx context.Context, id string) error
}

// 可以在这里添加具体的 MongoDB 或其他数据库实现
