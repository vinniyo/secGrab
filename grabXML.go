package secGrab

import (
	"database/sql"
	"encoding/xml"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"robinhood"

	"github.com/widuu/goini"
)

var conf = goini.SetConfig("./conf.ini")

func GrabXml(xmlFile, id *string, db *sql.DB) error {
	fmt.Println("Checking xml:", *xmlFile)
	resp, err := http.Get(*xmlFile)
	if err != nil {
		fmt.Println("Get sec main xml error:", err)
		return err
	}
	defer resp.Body.Close()

	content, _ := ioutil.ReadAll(resp.Body)

	//make doc
	var Data doc
	xml.Unmarshal(content, &Data)

	////////////////////////////////////////////////////////

	//fmt.Println("info:", Data.Issuer.IssuerTradingSymbol, Data.NonDerivativeTable.NonDerivativeTransaction.TransactionAmounts.TransactionPricePerShare.Value)

	date := time.Now().Format("20060102")

	Data.Issuer.IssuerTradingSymbol = strings.ToUpper(Data.Issuer.IssuerTradingSymbol)

	totalCost := Data.NonDerivativeTable.NonDerivativeTransaction.TransactionAmounts.TransactionShares.Value * Data.NonDerivativeTable.NonDerivativeTransaction.TransactionAmounts.TransactionPricePerShare.Value
	//fmt.Println("totalcost: ", totalCost)
	if totalCost == 0 {
		//need to have the return
		errorString := "Returning from Grabtext: sharePrice was " + fmt.Sprintf("%.2f", Data.NonDerivativeTable.NonDerivativeTransaction.TransactionAmounts.TransactionPricePerShare.Value) + " share amount: " + fmt.Sprintf("%.2f", Data.NonDerivativeTable.NonDerivativeTransaction.TransactionAmounts.TransactionShares.Value) + " for: " + Data.Issuer.IssuerTradingSymbol
		return errors.New(errorString)
	}

	if Data.NonDerivativeTable.NonDerivativeTransaction.TransactionAmounts.TransactionAcquiredDisposedCode.Value == "A" {

		//get quote from RobinHood API
		price, _ := robinhood.GetQuote(&Data.Issuer.IssuerTradingSymbol)
		mycost, _ := strconv.ParseFloat(price, 64)

		//diffresult calculates the different between the cost that the office purchsed for and what the actual cost is for me.
		diffResult := math.Abs(((mycost - Data.NonDerivativeTable.NonDerivativeTransaction.TransactionAmounts.TransactionPricePerShare.Value) / Data.NonDerivativeTable.NonDerivativeTransaction.TransactionAmounts.TransactionPricePerShare.Value) * 100)
		if diffResult > 12.00 {
			fmt.Println("Disguarding because our cost is ", mycost, " and their buy price is ", Data.NonDerivativeTable.NonDerivativeTransaction.TransactionAmounts.TransactionPricePerShare.Value, " for ", Data.Issuer.IssuerTradingSymbol)
			return errors.New("")
		}
		spy := getLastSandPPrice(db)

		purchased := 0
		priceWhenPurchased, sandpPriceWhenPurchased := filingPurchasedChecker(&Data.Issuer.IssuerTradingSymbol, db)
		if priceWhenPurchased != 0 {
			purchased = 1
		}

		//time.Sleep(time.Second * 1)
		//ADD RECORD TO DATABASE
		_, errExec := db.Exec("INSERT INTO Filing (ID, date, IssuerCik, IssuerName, IssuerTradingSymbol, RptOwnerName, RptOwnerCik, IsDirector, IsOfficer, IsTenPercentOwner, SecurityTitle, TransactionShares, TransactionPricePerShare, TransactionAcquiredDisposedCode, SharesOwnedFollowingTransaction, totalCost, PriceWhenFound, SandPPrice, CurrentPrice, Purchased, PriceWhenPurchased, SandPPriceWhenPurchased) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", *id, date, Data.Issuer.IssuerCik, Data.Issuer.IssuerName, Data.Issuer.IssuerTradingSymbol, Data.ReportingOwner.ReportingOwnerId.RptOwnerName, Data.ReportingOwner.ReportingOwnerId.RptOwnerCik, Data.ReportingOwner.ReportingOwnerRelationship.IsDirector, Data.ReportingOwner.ReportingOwnerRelationship.IsOfficer, Data.ReportingOwner.ReportingOwnerRelationship.IsTenPercentOwner, Data.NonDerivativeTable.NonDerivativeTransaction.SecurityTitle.Value, Data.NonDerivativeTable.NonDerivativeTransaction.TransactionAmounts.TransactionShares.Value, Data.NonDerivativeTable.NonDerivativeTransaction.TransactionAmounts.TransactionPricePerShare.Value, Data.NonDerivativeTable.NonDerivativeTransaction.TransactionAmounts.TransactionAcquiredDisposedCode.Value, Data.NonDerivativeTable.NonDerivativeTransaction.PostTransactionAmounts.SharesOwnedFollowingTransaction.Value, totalCost, price, spy, price, purchased, priceWhenPurchased, sandpPriceWhenPurchased)
		if errExec != nil {
			//ID in table is unique, which will reject all dupes
			fmt.Println("This is the error adding stock to database, which should only be duplicates", errExec)
			return errExec
		} else {
			fmt.Println("WE ADDED STOCK:", Data.Issuer.IssuerTradingSymbol, "was traded by:", Data.ReportingOwner.ReportingOwnerId.RptOwnerName, "for a total cost of:", totalCost)
		}

		//fmt.Println("Current price for: ", Data.Issuer.IssuerTradingSymbol)
	} else if Data.NonDerivativeTable.NonDerivativeTransaction.TransactionAmounts.TransactionAcquiredDisposedCode.Value == "D" {

		if conf.GetValue("global", "SellWhenSold") == "yes" {
			purchasedCount := purchasedChecker(&Data.Issuer.IssuerTradingSymbol, db)
			if purchasedCount != 0 {
				//Sell all records with that symbol THAT havent been sold yet. sold = 0
				_, errExec := db.Exec("Update Filing Set Sold = ? where IssuerTradingSymbol = ? AND Sold = 0", date, Data.Issuer.IssuerTradingSymbol)
				if errExec != nil {
					fmt.Println("THIS IS THE ERROR FOR Deleting records in Grabxml from a D:", errExec)
					return errExec
				} else {
					fmt.Println("we closed all positions:", Data.Issuer.IssuerTradingSymbol)
				}
				//////
				_, errExec2 := db.Exec("DELETE FROM Purchased WHERE symbol = ?", Data.Issuer.IssuerTradingSymbol)
				if errExec2 != nil {
					fmt.Println("THIS IS THE ERROR FOR Deleting records in grabxml purchasedChecker:", errExec2)
					return errExec2
				}
				//ACTUALLY SELL THE STOCK HERE!!!!!!
				if conf.GetValue("global", "live") == "yes" {
					quote, _ := robinhood.GetQuote(&Data.Issuer.IssuerTradingSymbol)
					quoteF64, errQuote2Float := strconv.ParseFloat(quote, 64)
					if errQuote2Float != nil {
						logMe(errQuote2Float.Error(), "./logERR.txt")
						return errQuote2Float
					}

					side := "sell"
					response, errPurchase := robinhood.Purchase(&Data.Issuer.IssuerTradingSymbol, &side, &purchasedCount, &quoteF64)
					if errPurchase != nil {
						logMe(errPurchase.Error(), "./logERR.txt")
						return errPurchase
					}
					logMe(response, "./logBuyResponse.txt")
				}

			}
		}

	}
	return nil
}

func getLastSandPPrice(db *sql.DB) float64 {
	var spprice float64
	errExec := db.QueryRow("SELECT Price FROM SandP WHERE SandP = 1").Scan(&spprice)
	if errExec != nil {
		fmt.Println("Here is the main query error:", errExec)
	}
	fmt.Println("HERE IS THE SandP Price FROM DB", spprice)
	//spyFloat64, _ := strconv.ParseFloat(spy, 64)
	return spprice
}

//this is to check if the stock we're adding is already purchased. If so, we mark it a purchased when adding to the Filings so we don't buy it again.
func filingPurchasedChecker(symbol *string, db *sql.DB) (float64, float64) {
	var priceWhenPurchased, sandpPriceWhenPurchased float64
	priceWhenPurchased, sandpPriceWhenPurchased = 0, 0
	err := db.QueryRow("SELECT PriceWhenPurchased, SandPPriceWhenPurchased FROM Filing WHERE Purchased = 1 And IssuerTradingSymbol = ? GROUP by IssuerTradingSymbol", *symbol).Scan(&priceWhenPurchased, &sandpPriceWhenPurchased)
	if err != nil {
		//fmt.Println("Didn't find any purchased, adding as unpurchased", *symbol, err)
		return 0, 0
	}
	fmt.Println("Found this stock which we had already, adding as purchased", *symbol)
	return priceWhenPurchased, sandpPriceWhenPurchased
}

func purchasedChecker(symbol *string, db *sql.DB) int {
	sharesPurchased := 0
	err := db.QueryRow("SELECT sharesPurchased FROM Purchased WHERE symbol = ?", *symbol).Scan(&sharesPurchased)
	if err != nil {
		return 0
	}
	fmt.Println("Found this stock as purchased", *symbol)
	return sharesPurchased
}

func logMe(text, filename string) {
	f, err := os.OpenFile(filename, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		fmt.Println("error opening file:", err)
	}
	defer f.Close()

	log.SetOutput(f)
	log.Println(text)
}

////////////////////////////Structs for text files
//////////////////////
/////////////////////

type reportingOwnerRelationship struct {
	XMLName           xml.Name `xml:"reportingOwnerRelationship"`
	IsDirector        bool     `xml:"isDirector"`
	IsOfficer         bool     `xml:"isOfficer"`
	IsTenPercentOwner bool     `xml:"isTenPercentOwner"`
}

type reportingOwnerId struct {
	XMLName      xml.Name `xml:"reportingOwnerId"`
	RptOwnerCik  int64    `xml:"rptOwnerCik"`
	RptOwnerName string   `xml:"rptOwnerName"`
}

type reportingOwner struct {
	XMLName                    xml.Name                   `xml:"reportingOwner"`
	ReportingOwnerId           reportingOwnerId           `xml:"reportingOwnerId"`
	ReportingOwnerRelationship reportingOwnerRelationship `xml:"reportingOwnerRelationship"`
}

type securityTitle struct {
	XMLName xml.Name `xml:"securityTitle"`
	Value   string   `xml:"value"`
}

type transactionShares struct {
	XMLName xml.Name `xml:"transactionShares"`
	Value   float64  `xml:"value"`
}

type transactionPricePerShare struct {
	XMLName xml.Name `xml:"transactionPricePerShare"`
	Value   float64  `xml:"value"`
}

type transactionAcquiredDisposedCode struct {
	XMLName xml.Name `xml:"transactionAcquiredDisposedCode"`
	Value   string   `xml:"value"`
}

type sharesOwnedFollowingTransaction struct {
	XMLName xml.Name `xml:"sharesOwnedFollowingTransaction"`
	Value   float64  `xml:"value"`
}

type postTransactionAmounts struct {
	XMLName                         xml.Name                        `xml:"postTransactionAmounts"`
	SharesOwnedFollowingTransaction sharesOwnedFollowingTransaction `xml:"sharesOwnedFollowingTransaction"`
}

type transactionAmounts struct {
	XMLName                         xml.Name                        `xml:"transactionAmounts"`
	TransactionShares               transactionShares               `xml:"transactionShares"`
	TransactionPricePerShare        transactionPricePerShare        `xml:"transactionPricePerShare"`
	TransactionAcquiredDisposedCode transactionAcquiredDisposedCode `xml:"transactionAcquiredDisposedCode"`
}

type nonDerivativeTransaction struct {
	XMLName                xml.Name               `xml:"nonDerivativeTransaction"`
	SecurityTitle          securityTitle          `xml:"securityTitle"`
	TransactionAmounts     transactionAmounts     `xml:"transactionAmounts"`
	PostTransactionAmounts postTransactionAmounts `xml:"postTransactionAmounts"`
}

type nonDerivativeTable struct {
	XMLName                  xml.Name                 `xml:"nonDerivativeTable"`
	NonDerivativeTransaction nonDerivativeTransaction `xml:"nonDerivativeTransaction"`
}

type derivativeTable struct {
	XMLName               xml.Name              `xml:"derivativeTable"`
	DerivativeTransaction derivativeTransaction `xml:"derivativeTransaction"`
}

type derivativeTransaction struct {
	XMLName                xml.Name               `xml:"derivativeTransaction"`
	SecurityTitle          securityTitle          `xml:"securityTitle"`
	TransactionAmounts     transactionAmounts     `xml:"transactionAmounts"`
	PostTransactionAmounts postTransactionAmounts `xml:"postTransactionAmounts"`
}

type issuer struct {
	XMLName             xml.Name `xml:"issuer"`
	IssuerCik           int      `xml:"issuerCik"`
	IssuerName          string   `xml:"issuerName"`
	IssuerTradingSymbol string   `xml:"issuerTradingSymbol"`
}

type doc struct {
	XMLName            xml.Name           `xml:"ownershipDocument"`
	Issuer             issuer             `xml:"issuer"`
	SchemaVersion      string             `xml:"schemaVersion"`
	DocumentType       int                `xml:"documentType"`
	ReportingOwner     reportingOwner     `xml:"reportingOwner"`
	NonDerivativeTable nonDerivativeTable `xml:"nonDerivativeTable,omitempty"`
	DerivativeTable    derivativeTable    `xml:"derivativeTable,omitempty"`
}
