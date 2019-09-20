package models

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"gotrading/bitflyer"
	"gotrading/config"
)

type SignalEvent struct {
	Time        time.Time `json:"time"`
	ProductCode string    `json:"product_code"`
	Side        string    `json:"side"`
	Price       float64   `json:"price"`
	Size        float64   `json:"size"`
}

func (s *SignalEvent) Save() bool {
	cmd := fmt.Sprintf("INSERT INTO %s (time, product_code, side, price, size) VALUES (?, ?, ?, ?, ?)", tableNameSignalEvents)
	_, err := DbConnection.Exec(cmd, s.Time.Format(time.RFC3339), s.ProductCode, s.Side, s.Price, s.Size)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			log.Println(err)
			return true
		}
		return false
	}
	return true
}

type SignalEvents struct {
	Signals []SignalEvent `json:"signals,omitempty"`
}

func NewSignalEvents() *SignalEvents {
	return &SignalEvents{}
}

func GetSignalEventsByCount(loadEvents int) *SignalEvents {
	cmd := fmt.Sprintf(`SELECT * FROM (
        SELECT time, product_code, side, price, size FROM %s WHERE product_code = ? ORDER BY time DESC LIMIT ? )
        ORDER BY time ASC;`, tableNameSignalEvents)
	rows, err := DbConnection.Query(cmd, config.Config.ProductCode, loadEvents)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var signalEvents SignalEvents
	for rows.Next() {
		var signalEvent SignalEvent
		rows.Scan(&signalEvent.Time, &signalEvent.ProductCode, &signalEvent.Side, &signalEvent.Price, &signalEvent.Size)
		signalEvents.Signals = append(signalEvents.Signals, signalEvent)
	}
	err = rows.Err()
	if err != nil {
		return nil
	}
	return &signalEvents
}

func GetSignalEventsAfterTime(timeTime time.Time) *SignalEvents {
	cmd := fmt.Sprintf(`SELECT * FROM (
                SELECT time, product_code, side, price, size FROM %s
                WHERE DATETIME(time) >= DATETIME(?)
                ORDER BY time DESC
            ) ORDER BY time ASC;`, tableNameSignalEvents)
	rows, err := DbConnection.Query(cmd, timeTime.Format(time.RFC3339))
	if err != nil {
		return nil
	}
	defer rows.Close()

	var signalEvents SignalEvents
	for rows.Next() {
		var signalEvent SignalEvent
		rows.Scan(&signalEvent.Time, &signalEvent.ProductCode, &signalEvent.Side, &signalEvent.Price, &signalEvent.Size)
		signalEvents.Signals = append(signalEvents.Signals, signalEvent)
	}
	return &signalEvents
}

func (s *SignalEvents) CanBuy(api *bitflyer.APIClient) bool {
	positions, err := api.GetPositions()
	if err != nil {
		log.Println(err)
		return false
	}
	collateral, err := api.GetCollateral()
	if err != nil {
		log.Println(err)
		return false
	}
	if positions.Side == "SELL" || (positions.Side == "" && collateral.Collateral > 0) {
		return true
	} else if positions.RequireCollateral < collateral.Collateral / 2 {
		return true
	} else {
		return false
	}
}

func (s *SignalEvents) CanSell(api *bitflyer.APIClient) bool {
	positions, err := api.GetPositions()
	if err != nil {
		log.Println(err)
		return false
	}
	collateral, err := api.GetCollateral()
	if err != nil {
		log.Println(err)
		return false
	}
	if positions.Side == "BUY" || positions.Side == "" {
		return true
	} else if positions.RequireCollateral < collateral.Collateral / 2 {
		return true
	} else {
		return false
	}
}

func (s *SignalEvents) Buy(ProductCode string, time time.Time, price, size float64, save bool) bool {
	api := bitflyer.New(config.Config.ApiKey, config.Config.ApiSecret)
	if !s.CanBuy(api) {
		return false
	}
	signalEvent := SignalEvent{
		ProductCode: ProductCode,
		Time:        time,
		Side:        "BUY",
		Price:       price,
		Size:        size,
	}
	if save {
		signalEvent.Save()
	}
	s.Signals = append(s.Signals, signalEvent)
	return true
}

func (s *SignalEvents) Sell(productCode string, time time.Time, price, size float64, save bool) bool {
	api := bitflyer.New(config.Config.ApiKey, config.Config.ApiSecret)
	if !s.CanSell(api) {
		return false
	}

	signalEvent := SignalEvent{
		ProductCode: productCode,
		Time:        time,
		Side:        "SELL",
		Price:       price,
		Size:        size,
	}
	if save {
		signalEvent.Save()
	}
	s.Signals = append(s.Signals, signalEvent)
	return true
}

func (s *SignalEvents) Profit(api *bitflyer.APIClient) float64 {
	total := 0.0
	ticker, _ := api.GetTicker(config.Config.ProductCode)
	price := ticker.GetMidPrice()
	position, _ := api.GetPositions()
	for i, signalEvent := range s.Signals {
		if i == 0 && signalEvent.Side == "SELL" {
			continue
		}
		if signalEvent.Side == "BUY" {
			total -= signalEvent.Price * signalEvent.Size
		}
		if signalEvent.Side == "SELL" {
			total += signalEvent.Price * signalEvent.Size
		}
	}
	if position.Side == "BUY" {
		total += price * position.Size
	} else if position.Side == "SELL" {
		total -= price *position.Size
	}
	return total
}

func (s SignalEvents) MarshalJSON() ([]byte, error) {
	value, err := json.Marshal(&struct {
		Signals []SignalEvent `json:"signals,omitempty"`
		Profit  float64       `json:"profit,omitempty"`
	}{
		Signals: s.Signals,
		Profit:  s.Profit(bitflyer.New(config.Config.ApiKey, config.Config.ApiSecret)),
	})
	if err != nil {
		return nil, err
	}
	return value, err
}

func (s *SignalEvents) CollectAfter(time time.Time) *SignalEvents {
	for i, signal := range s.Signals {
		if time.After(signal.Time) {
			continue
		}
		return &SignalEvents{Signals: s.Signals[i:]}
	}
	return nil
}
