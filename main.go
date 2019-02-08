package main

// https://www.domoticz.com/forum/viewtopic.php?t=1785 // virtual device?
// https://www.domoticz.com/forum/viewtopic.php?t=10940 // sonos
// https://github.com/jishi/node-sonos-http-api // sonos api
// https://www.domoticz.com/forum/viewtopic.php?t=11577 // update virtual device
// https://github.com/dhleong/ps4-waker/issues/14 // ps4 waker -> netflix

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/mxk/go-imap/imap"
)

type imapConfig struct {
	addrStr        *string
	loginStr       *string
	passwordStr    *string
	deleteAfterStr *string
	mailboxStr     *string
}

type domoticsConfig struct {
	urlStr      *string // http://localhost:8443
	pathStr     *string // /json.htm?type=command&param=updateuservariable&vname=alarm_state&vtype=2&vvalue=USERVARIABLEVALUE
	loginStr    *string
	passwordStr *string
}

var config imapConfig
var domotics domoticsConfig

var oldState string

func init() {
	config.addrStr = flag.String("addr", os.Getenv("IMAP_ADDR"), "imap address:port")
	config.loginStr = flag.String("login", os.Getenv("IMAP_LOGIN"), "imap login")
	config.passwordStr = flag.String("password", os.Getenv("IMAP_PASSWORD"), "imap password")
	config.mailboxStr = flag.String("mailbox", os.Getenv("IMAP_MAILBOX"), "imap mailbox")
	domotics.urlStr = flag.String("domotics-url", os.Getenv("DOMOTICS_URL"), "domotics url")
	domotics.pathStr = flag.String("domotics-path", os.Getenv("DOMOTICS_PATH"), "domotics path")
	domotics.loginStr = flag.String("domotics-login", os.Getenv("DOMOTICS_LOGIN"), "domotics login")
	domotics.passwordStr = flag.String("domotics-password", os.Getenv("DOMOTICS_PASSWORD"), "domotics password")
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
	if *domotics.urlStr == "" {
		t := "http://localhost:8443"
		domotics.urlStr = &t
	}
	if *domotics.pathStr == "" { // https://www.domoticz.com/wiki/User_variables
		t := "/json.htm?type=command&param=updateuservariable&vname=%s&vtype=2&vvalue=%s"
		domotics.pathStr = &t
	}
}

func main() {
	sigterm := make(chan os.Signal, 10)
	signal.Notify(sigterm, os.Interrupt, syscall.SIGTERM)

	for {
		fmt.Println("Main Loop")
		select {
		case <-sigterm:
			log.Println("Program killed by signal!")
			return
		default:
			err := getStatus()
			fmt.Printf("Exited: %s\n", err)
		}
	}

}

func getStatus() error {

	sigterm := make(chan os.Signal, 10)
	signal.Notify(sigterm, os.Interrupt, syscall.SIGTERM)

	imap.DefaultLogger = log.New(os.Stdout, "", 0)
	//imap.DefaultLogMask = imap.LogConn | imap.LogRaw
	imap.DefaultLogMask = imap.LogConn
	status := "UNKNOWN"

	c, err := Dial(*config.addrStr)
	if err != nil {
		return err
	}
	defer func() { ReportOK(c.Logout(1 * time.Second)) }()

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
	t := time.NewTimer(60 * time.Second)

	// Loop till error
	for {
		// sleep so we don't hammer the server
		time.Sleep(2 * time.Second)

		select {
		case <-sigterm:
			return fmt.Errorf("User quit")
		case <-t.C:
			return fmt.Errorf("login expired")
		default:
		}

		_, err := ReportOK(c.Noop())
		if err != nil {
			return err
		}

		// Find all mails from verisure
		cmd, err := ReportOK(c.UIDSearch("FROM", c.Quote("Verisure")))
		if err != nil {
			return err
		}
		r := cmd.Data[0].SearchResults()
		if len(r) == 0 {
			fmt.Println("No mail")
			ReportOK(c.Close(true))
			continue
		}
		fmt.Printf("%d mails: %v\n", len(r), r)
		status_by := "unknown"
		status_int := 0

		// Read last few messages, up to where we get a result
	L:
		for i := len(r) - 1; i >= 0; i-- {

			lastSet, _ := imap.NewSeqSet("")
			lastSet.AddNum(r[i])

			// Get Subject
			cmd, err = ReportOK(c.UIDFetch(lastSet, "FLAGS", "INTERNALDATE", "RFC822.SIZE", "BODY[HEADER.FIELDS (SUBJECT)]"))
			if err != nil {
				return err
			}
			z := string(imap.AsBytes(cmd.Data[0].MessageInfo().Attrs["BODY[HEADER.FIELDS (SUBJECT)]"]))
			z = strings.TrimSuffix(z, "\n")
			z = strings.TrimSuffix(z, "\r")
			z = strings.TrimSuffix(z, "\n")
			z = strings.TrimSuffix(z, "\r")
			z = strings.TrimPrefix(z, "Subject: ")

			// Get Body
			cmdBody, err := ReportOK(c.UIDFetch(lastSet, "FLAGS", "INTERNALDATE", "RFC822.SIZE", "BODY[]"))
			if err != nil {
				return err
			}
			body := string(imap.AsBytes(cmdBody.Data[0].MessageInfo().Attrs["BODY[]"]))
			//fmt.Printf("Body: [%s]\n", body)
			r, _ := regexp.Compile("Het systeem .* door (.*).")
			match := r.FindStringSubmatch(body)
			if len(match) > 0 {
				status_by = strings.TrimRight(match[1], ".")
				fmt.Printf("Match: _%+v_\n", match[1])
			}
			// Het systeem Bongerd 36 werd ingeschakeld met een Starkey door R. Doorn.

			// Parse subject
			switch z {
			case "Systeem uitgeschakeld", "Uitgeschakeld":
				status = "OFF"
				status_int = 0
				break L
			case "Systeem ingeschakeld":
				status = "ARMED_AWAY"
				status_int = 10
				break L
			case "Gedeeltelijk ingeschakeld":
				status = "ARMED_HOME"
				status_int = 20
				break L
			default:
				fmt.Printf("Unknown Status: [%s]\n", z)
				// remove message
			}
		}
		if oldState != status {
			PostUserData("alarm_state_by", status_by)
			err = PostUserData("alarm_state", status)
			if err == nil {
				oldState = status
			}
			err = PostPathData(fmt.Sprintf("/json.htm?type=command&param=switchlight&idx=%d&switchcmd=Set%%20Level&level=%d", 230, status_int))
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
	return nil
	//return status
	//ReportOK(c.Delete(mbox))
}

func Dial(addr string) (c *imap.Client, err error) {
	if strings.HasSuffix(addr, ":993") {
		c, err = imap.DialTLS(addr, &tls.Config{InsecureSkipVerify: true})
	} else {
		c, err = imap.Dial(addr)
	}
	return c, err
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

func ReportOK(cmd *imap.Command, err error) (*imap.Command, error) {
	cmd.Result(imap.OK)
	//return cmd
	//var rsp *imap.Response
	if cmd == nil {
		fmt.Printf("--- ??? ---\n%v\n\n", err)
		//panic(err)
		return cmd, err
	} else if err == nil {
		_, err = cmd.Result(imap.OK)
	}
	if err != nil {
		fmt.Printf("--- %s ---\n%v\n\n", cmd.Name(true), err)
		return cmd, err
		//panic(err)
	}
	c := cmd.Client()
		fmt.Printf("--- %s ---\n"+
			"%d command response(s), %d unilateral response(s)\n",
			cmd.Name(true), len(cmd.Data), len(c.Data))
		/*fmt.Printf("--- %s ---\n"+
			"%d command response(s), %d unilateral response(s)\n"+
			"%s %s\n\n",
			cmd.Name(true), len(cmd.Data), len(c.Data), rsp.Status, rsp.Info)*/
	c.Data = nil
	return cmd, nil
}

func PostUserData(variable string, state string) error {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	path := fmt.Sprintf(*domotics.pathStr, variable, UrlEncoded(state))
	url := fmt.Sprintf("%s%s", *domotics.urlStr, path)
	fmt.Printf("GET on: [%s]\n", url)
	req, err := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(*domotics.loginStr, *domotics.passwordStr)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	bodyText, err := ioutil.ReadAll(resp.Body)
	fmt.Printf("Output: %s", bodyText)
	return nil
}

func PostPathData(path string) error {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{Transport: tr}
	url := fmt.Sprintf("%s%s", *domotics.urlStr, path)
	fmt.Printf("GET on: [%s]\n", url)
	req, err := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(*domotics.loginStr, *domotics.passwordStr)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	bodyText, err := ioutil.ReadAll(resp.Body)
	fmt.Printf("Output: %s", bodyText)
	return nil
}

func UrlEncoded(str string) string {
	u, err := url.Parse(str)
	if err != nil {
		return ""
	}
	return u.String()
}
