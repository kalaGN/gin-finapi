package helloworld

import (
	"testing"
)

func TestHelloWorldProto(t *testing.T) {
	// 测试 HelloRequest
	req := &HelloRequest{Name: "Gin FinAPI"}
	if req.GetName() != "Gin FinAPI" {
		t.Errorf("Expected name 'Gin FinAPI', got '%s'", req.GetName())
	}

	// 测试 HelloReply
	rep := &HelloReply{Message: "Hello Gin FinAPI"}
	if rep.GetMessage() != "Hello Gin FinAPI" {
		t.Errorf("Expected message 'Hello Gin FinAPI', got '%s'", rep.GetMessage())
	}

	t.Log("✅ gRPC protobuf 代码生成成功！")
	t.Log("✅ HelloRequest 和 HelloReply 类型定义正常！")
}
