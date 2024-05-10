package client

import (
	"github.com/ao-data/albiondata-client/log"
)

type operationRealEstateBidOnAuction struct {
}

func (op operationRealEstateBidOnAuction) Process(state *albionState) {
	log.Debug("Got RealEstateBidOnAuction operation...")
}

type operationRealEstateBidOnAuctionResponse struct {
}

func (op operationRealEstateBidOnAuctionResponse) Process(state *albionState) {
	log.Debug("Got response to RealEstateBidOnAuction operation...")
}
