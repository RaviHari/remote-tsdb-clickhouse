package clickhouse

import (
	"context"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"slices"

	"github.com/prometheus/prometheus/prompb"
)

func (ch *ClickHouseAdapter) WriteRequest(ctx context.Context, req *prompb.WriteRequest) (int, error) {
	commitDone := false

	tx, err := ch.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if !commitDone {
			tx.Rollback()
		}
	}()

	// NOTE: Value of ch.table is sanitized in NewClickHouseAdapter.
	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf("INSERT INTO %s", ch.table))
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	count := 0

	for _, t := range req.Timeseries {
		var name string
		labels := make([]string, 0, len(t.Labels))
		//fmt.Println("timeseries metric:", t)
		for _, l := range t.Labels {
			if l.Name == "__name__" {
				name = l.Value
				continue
			}
			labels = append(labels, l.Name+"="+l.Value)
		}
		//fmt.Println("labels:", labels)
		//labelstring := strings.Join(labels, " ")
		count += len(t.Samples)
		for _, s := range t.Samples {
			_, err = stmt.Exec(
				time.UnixMilli(s.Timestamp).UTC(),
				name,
				labels,
				s.Value,
			)
			if err != nil {
				return 0, err
			}
		}
	}

	err = tx.Commit()
	commitDone = true
	return count, err
}

func (ch *ClickHouseAdapter) WriteOptimizedRequestSamples(ctx context.Context, req *prompb.WriteRequest) (int, error) {

	commitDone := false

	tx, err := ch.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if !commitDone {
			tx.Rollback()
		}
	}()

	// NOTE: Value of ch.samplesTable is sanitized in NewClickHouseAdapter.
	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf("INSERT INTO %s", ch.samplesTable))
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	count := 0

	for _, t := range req.Timeseries {
		var name string
		labels := make([]string, 0, len(t.Labels))
		//fmt.Println("timeseries metric:", t)
		for _, l := range t.Labels {
			if l.Name == "__name__" {
				name = l.Value
				continue
			}
			labels = append(labels, l.Name+"="+l.Value)
		}
		//fmt.Println("labels:", labels)
		labelString := strings.Join(labels, " ")
		fingerPrint := generateFingerPrint(labelString)
		//fmt.Println("fingerprint: ", fingerPrint)
		count += len(t.Samples)
		for _, s := range t.Samples {
			fmt.Println("timeseries sample:", s)
			_, err = stmt.Exec(
				time.UnixMilli(s.Timestamp).UTC(),
				name,
				fingerPrint,
				s.Value,
			)
			if err != nil {
				return 0, err
			}
		}
	}

	err = tx.Commit()
	commitDone = true
	return count, err
}

func (ch *ClickHouseAdapter) WriteOptimizedRequestTimeSeries(ctx context.Context, req *prompb.WriteRequest) (int, error) {

	commitDone := false

	tx, err := ch.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() {
		if !commitDone {
			tx.Rollback()
		}
	}()

	// NOTE: Value of ch.samplesTable is sanitized in NewClickHouseAdapter.
	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf("INSERT INTO %s", ch.timeSeriesTable))
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	count := 0
	fingerPrintList := []uint64{}
	for _, t := range req.Timeseries {
		var name string
		labels := make([]string, 0, len(t.Labels))
		//fmt.Println("timeseries metric:", t)
		for _, l := range t.Labels {
			if l.Name == "__name__" {
				name = l.Value
				continue
			}
			labels = append(labels, l.Name+"="+l.Value)
		}
		//fmt.Println("labels:", labels)
		labelString := strings.Join(labels, " ")
		fingerPrint := generateFingerPrint(labelString)
		bool := slices.Contains(fingerPrintList, fingerPrint)
		if !bool {
			fingerPrintList = append(fingerPrintList, fingerPrint)
		}
		fmt.Println("fingerprint: ", fingerPrint)
		count += 1
		_, err = stmt.Exec(
			name,
			fingerPrint,
			labelString,
		)
		if err != nil {
			return 0, err
		}
	}

	err = tx.Commit()
	commitDone = true
	return count, err
}

func (ch *ClickHouseAdapter) WriteOptimizedRequestTimeSeriesMap(ctx context.Context, req *prompb.WriteRequest) (int, error) {
	count := 0
	var err error
	for _, t := range req.Timeseries {
		var name string
		labelmap := make(map[string]string)
		labels := make([]string, 0, len(t.Labels))
		for _, l := range t.Labels {
			if l.Name == "__name__" {
				name = l.Value
				continue
			}
			labels = append(labels, l.Name+"="+l.Value)
		}
		labelString := strings.Join(labels, " ")
		fingerPrint := generateFingerPrint(labelString)
		// generate a map to be inserted in clickhouse db
		for _, lm := range t.Labels {
			if lm.Name == "__name__" {
				name = lm.Value
				labelmap[lm.Name] = lm.Value
				continue
			}
			labelmap[lm.Name] = lm.Value
		}
		sortedmap := sortedmap(labelmap)
		bo := ch.checkFingerPrintExists(fingerPrint)
		//bool := slices.Contains(fList, fingerPrint)
		//fmt.Println("sortedmap and found: ", sortedmap, bool, fList, fingerPrint)
		if !bo {
			//fList = append(fList, fingerPrint)
			//fmt.Println("Inserting fingerprint and labels, fList:", fList)
			err := ch.updateFingerPrint(name, fingerPrint)
			if err != nil {
				panic(err)
			}
			_, err = ch.writeDataToTableSeriesMap(name, fingerPrint, sortedmap)
			if err != nil {
				panic(err)
			}
		} else {
			continue
		}
		count += 1
	}
	//fmt.Println(" before return fList:", fList)
	return count, err
}

func generateFingerPrint(s string) uint64 {
	h := sha1.New()
	io.WriteString(h, s)
	bs := h.Sum(nil)
	fingerPrint := binary.LittleEndian.Uint64(bs)
	return fingerPrint
}

func sortedmap(m map[string]string) map[string]string {
	s := map[string]string{}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		s[k] = m[k]
		//fmt.Println(k, m[k])
	}
	return s
}

func (ch *ClickHouseAdapter) checkFingerPrintExists(fingerPrint uint64) bool {

	rows, err := ch.db.QueryContext(context.Background(), fmt.Sprintf("SELECT Fingerprint FROM %s WHERE Fingerprint=%d", ch.metricFingerPrint, fingerPrint))
	if err != nil {
		panic(err)
	}
	b := false
	//final := false
	// Iterate over the rows returned by the SELECT statement and print the results
	for rows.Next() {
		//var metricName string,
		var fingerPrintPresent uint64
		if err := rows.Scan(&fingerPrintPresent); err != nil {
			panic(err)
		}
		//fmt.Println("fingerPrintPresent:", fingerPrintPresent)
		if fingerPrint == fingerPrintPresent {
			b = true
		}
	}

	if err := rows.Err(); err != nil {
		fmt.Println("error:", err)
		return false
	}

	//rowsfromlabel, err := tx.QueryContext(context.Background(), fmt.Sprintf("SELECT Fingerprint FROM %s WHERE Fingerprint=%d", ch.timeSeriesTableMap, fingerPrint))
	//if err != nil {
	//	panic(err)
	//}
	//
	//// Iterate over the rows returned by the SELECT statement and print the results
	//for rowsfromlabel.Next() {
	//	//var metricName string,
	//	var fingerPrintPresent uint64
	//	if err := rowsfromlabel.Scan(&fingerPrintPresent); err != nil {
	//		panic(err)
	//	}
	//	//fmt.Println("fingerPrintPresent:", fingerPrintPresent)
	//	if fingerPrint == fingerPrintPresent && b == true {
	//		final = true
	//	}
	//}
	//
	//if err := rowsfromlabel.Err(); err != nil {
	//	fmt.Println("error:", err)
	//	return false
	//}

	return b
}

func (ch *ClickHouseAdapter) updateFingerPrint(name string, fingerPrint uint64) error {
	commitDone := false

	tx, err := ch.db.Begin()
	if err != nil {
		panic(err)
	}
	defer func() {
		if !commitDone {
			tx.Rollback()
		}
	}()
	// NOTE: Value of ch.timeSeriesTableMap is sanitized in NewClickHouseAdapter.
	stmt, err := tx.PrepareContext(context.Background(), fmt.Sprintf("INSERT INTO %s", ch.metricFingerPrint))
	if err != nil {
		panic(err)
	}
	defer stmt.Close()
	_, err = stmt.Exec(
		name,
		fingerPrint,
	)
	if err != nil {
		return err
	}

	err = tx.Commit()
	commitDone = true
	return err

}

func (ch ClickHouseAdapter) writeDataToTableSeriesMap(name string, fingerPrint uint64, smap map[string]string) (bool, error) {

	commitDone := false

	tx, err := ch.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() {
		if !commitDone {
			tx.Rollback()
		}
	}()

	stmt, err := tx.PrepareContext(context.Background(), fmt.Sprintf("INSERT INTO %s", ch.timeSeriesTableMap))
	if err != nil {
		return false, err
	}
	defer stmt.Close()
	_, err = stmt.Exec(
		name,
		fingerPrint,
		smap,
	)
	if err != nil {
		return false, err
	}

	err = tx.Commit()
	commitDone = true
	return true, nil
}

func contains(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}

	return false
}
