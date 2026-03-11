package main

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"io"
	"marketmonkey/event"
	"net/http"
	"os"
	"strconv"
	"strings"
)

func main() {
	b := NewBinanceFutureZip("BTCUSDT", "2025", "03", "")
	trades, err := b.DownloadAggTrades()
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(len(trades))
}

type BinanceFutureZip struct {
	symbol string
	year   string
	month  string
	day    string
}

func NewBinanceFutureZip(symbol string, year string, month string, day string) *BinanceFutureZip {
	return &BinanceFutureZip{
		symbol: strings.ToUpper(symbol),
		year:   year,
		month:  month,
		day:    day,
	}
}

// // https://data.binance.vision/data/futures/um/daily/bookDepth/BTCUSDT/BTCUSDT-bookDepth-2025-04-06.zip
// func (b *BinanceFutureZip) DownloadObDepth() error {
// 	url := fmt.Sprintf("https://data.binance.vision/data/futures/um/daily/bookDepth/%s/%s-bookDepth-%s-%s-%s.zip", b.symbol, b.symbol, b.year, b.month, b.day)
// 	fmt.Println("downloading...", url)
// 	resp, err := http.Get(url)
// 	if err != nil {
// 		return err
// 	}
// }

// https://data.binance.vision/data/futures/um/daily/aggTrades/BTCUSDT/BTCUSDT-aggTrades-2024-10-01.zip
func (b *BinanceFutureZip) DownloadAggTrades() ([]*event.Trade, error) {
	// if no day, use monthly
	var url string
	switch b.day {
	case "", "0", "00":
		url = fmt.Sprintf("https://data.binance.vision/data/futures/um/monthly/aggTrades/%s/%s-aggTrades-%s-%s.zip", b.symbol, b.symbol, b.year, b.month)
	default:
		url = fmt.Sprintf("https://data.binance.vision/data/futures/um/daily/aggTrades/%s/%s-aggTrades-%s-%s-%s.zip", b.symbol, b.symbol, b.year, b.month, b.day)
	}

	csvFile, err := b.downloadAndUnzip(url)
	if err != nil {
		return nil, err
	}
	defer csvFile.Close()
	reader := csv.NewReader(csvFile)
	// [agg_trade_id price quantity first_trade_id last_trade_id transact_time is_buyer_maker]
	header, err := reader.Read()
	if err != nil {
		return nil, err
	}
	fmt.Println(header)
	trades := []*event.Trade{}
	for {
		row, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		trades = append(trades, b.parseAggTrade(row))
	}
	fmt.Println("received total trades", len(trades))
	return trades, nil
}

// [agg_trade_id price quantity first_trade_id last_trade_id transact_time is_buyer_maker]
func (b *BinanceFutureZip) parseAggTrade(row []string) *event.Trade {
	price, err := strconv.ParseFloat(row[1], 64)
	if err != nil {
		return nil
	}
	qty, err := strconv.ParseFloat(row[2], 64)
	if err != nil {
		return nil
	}
	unix, err := strconv.ParseInt(row[5], 10, 64)
	if err != nil {
		return nil
	}
	isBuy := strings.ToUpper(row[6]) == "TRUE"
	pair := &event.Pair{
		Exchange: "binancef",
		Symbol:   b.symbol,
	}
	return &event.Trade{
		Price: price,
		Qty:   qty,
		IsBuy: isBuy,
		Unix:  unix,
		Pair:  pair,
	}
}

// make sure to close when calling this function
// csv, err := b.downloadAndUnzip(url)
// defer csv.Close()
func (b *BinanceFutureZip) downloadAndUnzip(url string) (io.ReadCloser, error) {
	fmt.Println("downloading...", url)
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	fmt.Println("unziping")
	defer resp.Body.Close()

	dataDir := "./backfill"
	os.MkdirAll(dataDir, 0755)

	zipName := fmt.Sprintf("%s/%s-%s-%s-%s.zip", dataDir, b.symbol, b.year, b.month, b.day)
	zipFile, err := os.Create(zipName)
	if err != nil {
		return nil, err
	}
	defer zipFile.Close()
	_, err = io.Copy(zipFile, resp.Body)
	if err != nil {
		return nil, err
	}
	fileInfo, err := zipFile.Stat()
	if err != nil {
		return nil, err
	}
	zipReader, err := zip.NewReader(zipFile, fileInfo.Size())
	if err != nil {
		return nil, err
	}
	if len(zipReader.File) == 0 {
		return nil, fmt.Errorf("no files found in zip")
	}
	csvFile, err := os.Create(zipReader.File[0].Name)
	if err != nil {
		return nil, err
	}
	fileReader, err := zipReader.File[0].Open()
	if err != nil {
		csvFile.Close()
		return nil, err
	}
	_, err = io.Copy(csvFile, fileReader)
	fileReader.Close()
	if err != nil {
		csvFile.Close()
		return nil, err
	}

	_, err = csvFile.Seek(0, 0)
	if err != nil {
		csvFile.Close()
		return nil, err
	}
	return csvFile, nil
}
