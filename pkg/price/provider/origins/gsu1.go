//  Copyright (C) 2020 Maker Ecosystem Growth Holdings, INC.
//
//  This program is free software: you can redistribute it and/or modify
//  it under the terms of the GNU Affero General Public License as
//  published by the Free Software Foundation, either version 3 of the
//  License, or (at your option) any later version.
//
//  This program is distributed in the hope that it will be useful,
//  but WITHOUT ANY WARRANTY; without even the implied warranty of
//  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
//  GNU Affero General Public License for more details.
//
//  You should have received a copy of the GNU Affero General Public License
//  along with this program.  If not, see <http://www.gnu.org/licenses/>.

package origins

import (
        "encoding/json"
        "fmt"
        "github.com/chronicleprotocol/oracle-suite/pkg/util/query"
        "time"
        "strconv"
        "strings"
)

// GSU1 URL
const gsu1URL = "https://api.gsucoin.app/Products/GSULive/?symbol=%s"

type gsu1Response struct {
        Price     string    `json:"price"`
        Ask       string    `json:"ask"`
        Bid       string    `json:"bid"`
        Volume    string    `json:"volume"`
        Symbol    string    `json:"symbol"`
}

// GSU1 exchange handler
type GSU1 struct {
        WorkerPool query.WorkerPool
}

func (h *GSU1) localPairName(pair Pair) string {
        return strings.ToUpper(pair.Base)
}

func (h *GSU1) getURL(pairs []Pair) string {
        pairsStr := make([]string, len(pairs))
        for i, pair := range pairs {
                pairsStr[i] = h.localPairName(pair)
        }
        return fmt.Sprintf(gsu1URL, strings.Join(pairsStr, ","))
}

func (h GSU1) Pool() query.WorkerPool {
        return h.WorkerPool
}

func (h GSU1) PullPrices(pairs []Pair) []FetchResult {
        crs, err := h.fetch(pairs)
        if err != nil {
                return fetchResultListWithErrors(pairs, err)
        }
        return crs
}

func (h *GSU1) fetch(pairs []Pair) ([]FetchResult, error) {

        crs := make([]FetchResult, len(pairs))
        respMap := map[string]gsu1Response{}
        for i, pair := range pairs {
                symbol := h.localPairName(pair)
                url := fmt.Sprintf(gsu1URL, symbol)
                req := &query.HTTPRequest{
                        URL: url,
                }
                // make query
                res := h.Pool().Query(req)
                if res == nil {
                        return nil, ErrEmptyOriginResponse
                }
                if res.Error != nil {
                        return nil, res.Error
                }
                // parsing JSON
                var resp gsu1Response
                err := json.Unmarshal(res.Body, &resp)
                if err != nil {
                        return nil, fmt.Errorf("failed to parse GSU1 response: %w", err)
                }
                respMap[symbol] = resp

                if resp, has := respMap[pair.Base]; has {
                        p, err := h.newPrice(pair, resp)
                        if err != nil {
                                crs[i] = fetchResultWithError(
                                        pair,
                                        fmt.Errorf("failed to create price point from GSU1 response: %w: %s", err, res.Body),
                                )
                        } else {
                                crs[i] = fetchResult(p)
                        }
                } else {
                        crs[i] = fetchResultWithError(
                                pair,
                                fmt.Errorf("failed to find symbol %s in GSU1 response: %s", pair, res.Body),
                        )
                }
        }
        return crs, nil
}

func (h *GSU1) newPrice(pair Pair, resp gsu1Response) (Price, error) {
        // Parsing price from string.
        price, err := strconv.ParseFloat(resp.Price, 64)
        if err != nil {
                return Price{}, fmt.Errorf("failed to parse price from hitbtc exchange")
        }
        // Parsing ask from string.
        ask, err := strconv.ParseFloat(resp.Ask, 64)
        if err != nil {
                return Price{}, fmt.Errorf("failed to parse ask from hitbtc exchange")
        }
        // Parsing volume from string.
        volume, err := strconv.ParseFloat(resp.Volume, 64)
        if err != nil {
                return Price{}, fmt.Errorf("failed to parse volume from hitbtc exchange")
        }
        // Parsing bid from string.
        bid, err := strconv.ParseFloat(resp.Bid, 64)
        if err != nil {
                return Price{}, fmt.Errorf("failed to parse bid from hitbtc exchange")
        }
        // Building Price.
        return Price{
                Pair:      pair,
                Price:     price,
                Ask:       ask,
                Bid:       bid,
                Volume24h: volume,
                Timestamp: time.Now(),
        }, nil
}
