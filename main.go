package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/chzyer/readline"
	"github.com/fatih/color"
	"github.com/legolord208/stdutil"
)

const AutoRunFile = ".autorun"
const Version = "1.22.4dev"

var DevVersion = strings.Contains(Version, "dev")

var UserId string
var IsUser bool

var rl *readline.Instance
var ColorDefault = color.New(color.Bold)
var ColorAutomated = color.New(color.Italic)
var ColorError = color.New(color.FgRed, color.Bold)

const MSG_LIMIT = 2000

type stringArr []string

func (arr *stringArr) Set(val string) error {
	*arr = append(*arr, val)
	return nil
}

func (arr *stringArr) String() string {
	return "[" + strings.Join(*arr, " ") + "]"
}

func main() {
	var token string
	var email string
	var pass string
	var commands stringArr

	var noupdate bool
	var noautorun bool

	flag.StringVar(&token, "t", "", "Set token. Ignored if -e and/or -p are set.")
	flag.StringVar(&email, "e", "", "Set email.")
	flag.StringVar(&pass, "p", "", "Set password.")
	flag.Var(&commands, "x", "Pre-execute command. Can use flag multiple times.")

	flag.BoolVar(&noupdate, "noupdate", false, "Disable update checking.")
	flag.BoolVar(&noautorun, "noautorun", false, "Disable running commands in "+AutoRunFile+" file.")
	flag.Parse()

	doHook()
	fmt.Println("DiscordConsole " + Version)

	if !noupdate {
		fmt.Print("Checking for updates... ")
		update, err := checkUpdate()
		if err != nil {
			stdutil.PrintErr("Error checking for updates", err)
		} else {
			if update.UpdateAvailable {
				fmt.Println()
				if DevVersion {
					fmt.Println("Latest stable release: " + update.Version + ".")
				} else {
					color.Cyan("Update available: Version " + update.Version + ".")
				}
				color.Cyan("Download from " + update.Url + ".")
			} else {
				fmt.Println("No updates found.")
			}
		}
	}

	fmt.Println("Reading bookmarks...")
	err := loadBookmarks()
	if err != nil {
		stdutil.PrintErr("Could not read bookmarks", err)
	}

	var ar_lines []string
	if !noautorun {
		ar, err := ioutil.ReadFile(AutoRunFile)
		if err != nil && os.IsExist(err) {
			stdutil.PrintErr("Could not read "+AutoRunFile, err)
		} else if err == nil {
			ar_lines = strings.Split(string(ar), "\n")

			if len(ar_lines) > 0 {
				first_line := ar_lines[0]
				if strings.HasPrefix(first_line, ":") {
					token = first_line[1:]
					ar_lines = ar_lines[1:]
				}
			}
		}
	}

	rl, err = readline.New(EMPTY_POINTER)
	if err != nil {
		stdutil.PrintErr("Could not start readline library", err)
		return
	}

	if token == "" && email == "" && pass == "" {
		foundtoken, err := findToken()
		if err == nil {
			for {
				color.Set(color.FgYellow)
				fmt.Print("You are logged into Discord. Use that login? (y/n): ")
				response := stdutil.MustScanTrim()
				color.Unset()

				if strings.EqualFold(response, "y") {
					foundtoken = strings.TrimPrefix(foundtoken, "\"")
					foundtoken = strings.TrimSuffix(foundtoken, "\"")
					token = "user " + foundtoken
				} else if !strings.EqualFold(response, "n") {
					stdutil.PrintErr("Please type either 'y' or 'n'.", nil)
					continue
				}
				break
			}
		}
	}

	fmt.Println("Please paste your 'token' here, or leave blank for a username/password prompt.")
	fmt.Print("> ")
	if token == "" && email == "" && pass == "" {
		token, err = rl.Readline()
		if err != nil {
			if err != io.EOF && err != readline.ErrInterrupt {
				stdutil.PrintErr("Could not read line", err)
			}
			return
		}
	} else {
		if email != "" || pass != "" {
			token = ""
		}
		fmt.Println("[HIDDEN]")
	}

	var session *discordgo.Session
	if token == "" {
		IsUser = true

		rl.SetPrompt("Email: ")
		if email == "" {
			email, err = rl.Readline()
		} else {
			fmt.Println(email)
		}

		if pass == "" {
			pass2, err := rl.ReadPassword("Password: ")
			fmt.Println()

			if err != nil {
				if err != io.EOF && err != readline.ErrInterrupt {
					stdutil.PrintErr("Could not read password", err)
				}
				return
			}
			pass = string(pass2)
		}

		fmt.Println("Authenticating...")
		session, err = discordgo.New(email, pass)
	} else {
		fmt.Println("Authenticating...")
		if strings.HasPrefix(strings.ToLower(token), "user ") {
			token = token[len("user "):]
			IsUser = true
		} else {
			token = "Bot " + token
			IsUser = false
			intercept = false
		}
		session, err = discordgo.New(token)
	}

	if err != nil {
		stdutil.PrintErr("Couldn't authenticate", err)
		return
	}

	user, err := session.User("@me")
	if err != nil {
		stdutil.PrintErr("Couldn't query user", err)
		return
	}

	UserId = user.ID

	session.AddHandler(messageCreate)
	err = session.Open()
	if err != nil {
		stdutil.PrintErr("Could not open session", err)
	}

	fmt.Println("Logged in with user ID " + UserId)
	fmt.Println("Write 'help' for help")
	fmt.Println("Press Ctrl+D or type 'exit' to exit.")

	for i := 0; i < 3; i++ {
		fmt.Println()
	}

	go func() {
		c := make(chan os.Signal, 2)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)

		<-c
		exit(session)
	}()

	ColorAutomated.Set()

	if ar_lines != nil {
		for _, cmd := range ar_lines {
			cmd = strings.TrimSpace(cmd)
			if cmd == "" {
				continue
			}
			printPointer(session)
			fmt.Println(cmd)

			command(session, cmd)
		}
	}
	for _, cmd := range commands {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}
		printPointer(session)
		fmt.Println(cmd)

		command(session, cmd)
	}

	color.Unset()
	setCompleter(rl)

	for {
		ColorDefault.Set()

		rl.SetPrompt(pointer(session))
		cmd, err := rl.Readline()

		color.Unset()

		if err != nil {
			if err != io.EOF && err != readline.ErrInterrupt {
				stdutil.PrintErr("Could not read line", err)
			} else {
				fmt.Println("exit")
			}
			exit(session)
			return
		}

		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}

		command(session, cmd)
	}
}

func exit(session *discordgo.Session) {
	color.Unset()
	playing = ""
	session.Close()
	os.Exit(0)
}

func execute(command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func printMessage(session *discordgo.Session, msg *discordgo.Message, prefixR bool, guild *discordgo.Guild, channel *discordgo.Channel) {
	var s string
	if prefixR {
		s += "\r"
	}
	s += "("

	if channel.IsPrivate {
		s += "Private"
	} else {
		s += guild.Name + " " + "#" + channel.Name
	}

	s += ") " + msg.Author.Username + ": " + msg.Content
	s += strings.Repeat(" ", 5)

	color.Unset()
	color.Yellow(s)
	ColorDefault.Set()
}

const EMPTY_POINTER = "> "

var pointerCache string

func printPointer(session *discordgo.Session) {
	fmt.Print(pointer(session))
}
func pointer(session *discordgo.Session) string {
	if pointerCache != "" {
		return pointerCache
	}

	if loc.channel == nil {
		return EMPTY_POINTER
	}

	s := ""

	if loc.channel.IsPrivate {
		recipient := "Unknown"
		if loc.channel.Recipient != nil {
			recipient = loc.channel.Recipient.Username
		}
		s += "Private (" + recipient + ")"
	} else {
		guild := ""
		if loc.guild != nil {
			guild = loc.guild.Name
		}
		s += guild + " (#" + loc.channel.Name + ")"
	}

	s += EMPTY_POINTER
	pointerCache = s
	return s
}
