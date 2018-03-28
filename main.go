package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/mxk/go-imap/imap"
)

type imapConfig struct {
	addrStr        *string
	loginStr       *string
	passwordStr    *string
	deleteAfterStr *string
	mailboxStr     *string
	deleteAfterDur time.Duration
}

var config imapConfig

func init() {
	config.addrStr = flag.String("addr", os.Getenv("IMAP_ADDR"), "imap address:port")
	config.loginStr = flag.String("login", os.Getenv("IMAP_LOGIN"), "imap login")
	config.passwordStr = flag.String("password", os.Getenv("IMAP_PASSWORD"), "imap password")
	config.mailboxStr = flag.String("mailbox", os.Getenv("IMAP_MAILBOX"), "imap mailbox")
	config.deleteAfterStr = flag.String("delet-eafter", os.Getenv("IMAP_DELETE_AFTER"), "imap delete mails after")
	flag.Parse()
	if *config.addrStr == "" {
		flag.Usage()
		log.Fatal("address required!")
	}
	if *config.loginStr == "" {
		flag.Usage()
		log.Fatal("login required!")
	}
	if *config.passwordStr == "" {
		flag.Usage()
		log.Fatal("password required!")
	}
	if *config.passwordStr == "" {
		box := "INBOX"
		config.mailboxStr = &box
	}
	if *config.deleteAfterStr != "" {
		var err error
		config.deleteAfterDur, err = time.ParseDuration(*config.deleteAfterStr)
		if err != nil {
			log.Fatalf("could not convert %s in to a duration: %s", *config.deleteAfterStr, err)
		}
	}
}

func main() {
	getStatus()
}

func getStatus() {
	imap.DefaultLogger = log.New(os.Stdout, "", 0)
	//imap.DefaultLogMask = imap.LogConn | imap.LogRaw
	imap.DefaultLogMask = imap.LogConn
	status := "UNKNOWN"

	c := Dial(*config.addrStr)
	defer func() { ReportOK(c.Logout(30 * time.Second)) }()

	if c.Caps["STARTTLS"] {
		ReportOK(c.StartTLS(nil))
	}

	if c.Caps["ID"] {
		ReportOK(c.ID("name", "goimap"))
	}

	ReportOK(c.Noop())
	ReportOK(Login(c, *config.loginStr, *config.passwordStr))

	// Select INBOX
	ReportOK(c.Select("INBOX", false))

	// Loop till error
	for {
		// sleep so we don't hammer the server
		time.Sleep(1 * time.Second)

		// Find all mails from verisure
		cmd := ReportOK(c.UIDSearch("FROM", c.Quote("Verisure")))
		r := cmd.Data[0].SearchResults()
		if len(r) == 0 {
			fmt.Println("No mail")
			ReportOK(c.Close(true))
			continue
		}
		fmt.Printf("%d mails: %v\n", len(r), r)

		// Read last few messages, up to where we get a result
	L:
		for i := len(r) - 1; i >= 0; i-- {

			lastSet, _ := imap.NewSeqSet("")
			lastSet.AddNum(r[i])

			cmd = ReportOK(c.UIDFetch(lastSet, "FLAGS", "INTERNALDATE", "RFC822.SIZE", "BODY[HEADER.FIELDS (SUBJECT)]"))
			z := string(imap.AsBytes(cmd.Data[0].MessageInfo().Attrs["BODY[HEADER.FIELDS (SUBJECT)]"]))
			z = strings.TrimSuffix(z, "\n")
			z = strings.TrimSuffix(z, "\r")
			z = strings.TrimSuffix(z, "\n")
			z = strings.TrimSuffix(z, "\r")
			z = strings.TrimPrefix(z, "Subject: ")

			switch z {
			case "Uitgeschakeld":
				status = "OFF"
				break L
			case "Systeem ingeschakeld":
				status = "ARMED"
				break L
			case "Gedeeltelijk Ingeschakeld":
				status = "PARTIAL_ARMED"
				break L
			default:
				fmt.Printf("Unknown Status: [%s]\n", z)
				// remove message
			}
		}
		fmt.Printf("Status set to %s\n", status)
		// read only the last message
		/*
			last := r[len(r)-1]
			lastSet.AddNum(last)

			// remove any older verisure messages if they exist
			if len(r) > 1 {
				old := r[:len(r)-1]
				fmt.Printf("Removing old messages: %+v\n", old)
				oldSet, _ := imap.NewSeqSet("")
				oldSet.AddNum(old...)
				ReportOK(c.UIDStore(oldSet, "+FLAGS.SILENT", imap.NewFlagSet(`\Deleted`)))
				ReportOK(c.Expunge(nil))
			}*/
		//fmt.Printf("CMD: %+v SET: %+v\n", cmd, set)

		//cmd = ReportOK(c.Fetch(set, "FLAGS", "INTERNALDATE", "RFC822.SIZE", "BODY.PEEK[HEADER.FIELDS (SUBJECT)]"))
		//cmd = ReportOK(c.Fetch(set, "FLAGS", "INTERNALDATE", "RFC822.SIZE", "BODY[HEADER.FIELDS (SUBJECT)]"))
		//ReportOK(c.Fetch(set, "FLAGS", "INTERNALDATE", "RFC822.SIZE", "BODY[]"))
		//ReportOK(c.UIDStore(set, "+FLAGS.SILENT", imap.NewFlagSet(`\Deleted`)))
		//ReportOK(c.Expunge(nil))
		//ReportOK(c.UIDSearch("SUBJECT", c.Quote("GoIMAP")))

		//fmt.Println(c.Mailbox)

	}
	ReportOK(c.Close(true))
	//return status
	//ReportOK(c.Delete(mbox))
}

func Dial(addr string) (c *imap.Client) {
	var err error
	if strings.HasSuffix(addr, ":993") {
		c, err = imap.DialTLS(addr, &tls.Config{InsecureSkipVerify: true})
	} else {
		c, err = imap.Dial(addr)
	}
	if err != nil {
		panic(err)
	}
	return c
}

func Login(c *imap.Client, user, pass string) (cmd *imap.Command, err error) {
	defer c.SetLogMask(Sensitive(c, "LOGIN"))
	return c.Login(user, pass)
}

func Sensitive(c *imap.Client, action string) imap.LogMask {
	mask := c.SetLogMask(imap.LogConn)
	hide := imap.LogCmd | imap.LogRaw
	if mask&hide != 0 {
		c.Logln(imap.LogConn, "Raw logging disabled during", action)
	}
	c.SetLogMask(mask &^ hide)
	return mask
}

func ReportOK(cmd *imap.Command, err error) *imap.Command {
	cmd.Result(imap.OK)
	return cmd
	var rsp *imap.Response
	if cmd == nil {
		//fmt.Printf("--- ??? ---\n%v\n\n", err)
		panic(err)
	} else if err == nil {
		rsp, err = cmd.Result(imap.OK)
	}
	if err != nil {
		fmt.Printf("--- %s ---\n%v\n\n", cmd.Name(true), err)
		panic(err)
	}
	c := cmd.Client()
	fmt.Printf("--- %s ---\n"+
		"%d command response(s), %d unilateral response(s)\n"+
		"%s %s\n\n",
		cmd.Name(true), len(cmd.Data), len(c.Data), rsp.Status, rsp.Info)
	c.Data = nil
	return cmd
}
