package server

import (
	"context"
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	grpchealth "google.golang.org/grpc/health"
	grpchealthv1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/skyoo2003/kvs"
	"github.com/skyoo2003/kvs/api/kvsv1"
)

type kvStoreServer struct {
	kvsv1.UnimplementedKVStoreServer
	store *kvs.Store
}

func NewGRPCServer(store *kvs.Store) *grpc.Server {
	if store == nil {
		store = kvs.NewStore()
	}

	grpcServer := grpc.NewServer()
	kvsv1.RegisterKVStoreServer(grpcServer, &kvStoreServer{store: store})

	healthServer := grpchealth.NewServer()
	healthServer.SetServingStatus("", grpchealthv1.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus(kvsv1.KVStore_ServiceDesc.ServiceName, grpchealthv1.HealthCheckResponse_SERVING)
	grpchealthv1.RegisterHealthServer(grpcServer, healthServer)

	return grpcServer
}

func (s *kvStoreServer) Get(_ context.Context, req *kvsv1.GetRequest) (*kvsv1.GetResponse, error) {
	if req.GetKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	value, err := s.store.Get(req.GetKey())
	if err != nil {
		if errors.Is(err, kvs.ErrKeyNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}

		return nil, status.Error(codes.Internal, "failed to read key")
	}

	stringValue, ok := value.(string)
	if !ok {
		return nil, status.Error(codes.Internal, "stored value is not a string")
	}

	return &kvsv1.GetResponse{Key: req.GetKey(), Value: stringValue}, nil
}

func (s *kvStoreServer) Put(_ context.Context, req *kvsv1.PutRequest) (*kvsv1.PutResponse, error) {
	if req.GetKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	if err := s.store.Put(req.GetKey(), req.GetValue()); err != nil {
		return nil, status.Error(codes.Internal, "failed to store key")
	}

	return &kvsv1.PutResponse{}, nil
}

func (s *kvStoreServer) Delete(_ context.Context, req *kvsv1.DeleteRequest) (*kvsv1.DeleteResponse, error) {
	if req.GetKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "key is required")
	}

	if err := s.store.Delete(req.GetKey()); err != nil {
		if errors.Is(err, kvs.ErrKeyNotFound) {
			return nil, status.Error(codes.NotFound, err.Error())
		}

		return nil, status.Error(codes.Internal, "failed to delete key")
	}

	return &kvsv1.DeleteResponse{}, nil
}
