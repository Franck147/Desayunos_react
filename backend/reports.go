package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

func validDate(date string) bool {
	_, e := time.Parse("2006-01-02", date)
	return e == nil && len(date) == 10
}

type ProductSale struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Units  int    `json:"units"`
	Amount Money  `json:"amount"`
}
type Report struct {
	Date         string           `json:"date"`
	Revenue      Money            `json:"revenue"`
	PaidOrders   int              `json:"paid_orders"`
	PaidUnits    int              `json:"paid_units"`
	Completed    int              `json:"completed_orders"`
	Cancelled    int              `json:"cancelled_orders"`
	Unpaid       int              `json:"unpaid_orders"`
	UnpaidAmount Money            `json:"unpaid_amount"`
	Methods      map[string]Money `json:"by_method"`
	Products     []ProductSale    `json:"products"`
	Payments     []Order          `json:"payments"`
}

func buildReport(orders []Order, date string) Report {
	r := Report{Date: date, Methods: map[string]Money{"CASH": 0, "YAPE": 0, "PLIN": 0}, Products: []ProductSale{}, Payments: []Order{}}
	products := map[string]ProductSale{}
	for _, o := range orders {
		if businessDate(o.Updated) == date {
			if o.Status == "DELIVERED" {
				r.Completed++
			}
			if o.Status == "CANCELLED" {
				r.Cancelled++
			}
		}
		if o.Payment == nil && o.Status != "CANCELLED" && businessDate(o.Created) == date {
			r.Unpaid++
			r.UnpaidAmount += o.Total
		}
		if o.Payment == nil || businessDate(o.Payment.Created) != date {
			continue
		}
		r.Payments = append(r.Payments, o)
		r.PaidOrders++
		r.Revenue += o.Payment.Amount
		r.Methods[o.Payment.Method] += o.Payment.Amount
		for _, l := range o.Items {
			r.PaidUnits += l.Quantity
			id := l.ProductID
			if id == "" {
				id = l.Name
			}
			p := products[id]
			p.ID = id
			p.Name = l.Name
			p.Units += l.Quantity
			p.Amount += l.Price * Money(l.Quantity)
			products[id] = p
		}
	}
	for _, p := range products {
		r.Products = append(r.Products, p)
	}
	sort.Slice(r.Products, func(i, j int) bool {
		if r.Products[i].Units == r.Products[j].Units {
			return r.Products[i].Name < r.Products[j].Name
		}
		return r.Products[i].Units > r.Products[j].Units
	})
	sort.Slice(r.Payments, func(i, j int) bool { return r.Payments[i].Payment.Created > r.Payments[j].Payment.Created })
	return r
}
func writeCSV(w io.Writer, r Report) {
	_, _ = io.WriteString(w, "\uFEFF")
	csvWriter := csv.NewWriter(w)
	csvWriter.Comma = ';'
	csvWriter.UseCRLF = true
	cell := func(s string) string {
		if strings.ContainsAny(strings.TrimSpace(s)[:min(1, len(strings.TrimSpace(s)))], "=+-@") {
			return "'" + s
		}
		return s
	}
	_ = csvWriter.Write([]string{"Reporte de desayunos", r.Date, "America/Lima"})
	_ = csvWriter.Write([]string{"Cobros confirmados PEN", fmt.Sprintf("%.2f", float64(r.Revenue)/100)})
	_ = csvWriter.Write([]string{"Pedido", "Cliente", "Método", "Importe PEN", "Estado"})
	for _, o := range r.Payments {
		_ = csvWriter.Write([]string{fmt.Sprint(o.Number), cell(o.Name), o.Payment.Method, fmt.Sprintf("%.2f", float64(o.Payment.Amount)/100), o.Status})
	}
	_ = csvWriter.Write([]string{"Producto", "Unidades cobradas", "Importe PEN"})
	for _, p := range r.Products {
		_ = csvWriter.Write([]string{cell(p.Name), fmt.Sprint(p.Units), fmt.Sprintf("%.2f", float64(p.Amount)/100)})
	}
	csvWriter.Flush()
}
