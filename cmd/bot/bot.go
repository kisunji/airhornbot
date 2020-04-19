package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	log "github.com/sirupsen/logrus"
)

var (
	// discordgo session
	discord *discordgo.Session

	// Map of Guild id's to *Play channels, used for queuing and rate-limiting guilds
	queues = make(map[string][]*Play)

	MAX_QUEUE_SIZE = 6

	m sync.Mutex
)

// Play represents an individual use of the !airhorn command
type Play struct {
	GuildID   string
	ChannelID string
	Sound     *Sound

	// The next play to occur after this, only used for chaining sounds like anotha
	Next *Play

	// If true, this was a forced play using a specific airhorn sound name
	Forced bool
}

type SoundCollection struct {
	Prefix    string
	Commands  []string
	Sounds    []*Sound
	ChainWith *SoundCollection

	soundRange int
}

// Sound represents a sound clip
type Sound struct {
	Name string

	// Weight adjust how likely it is this song will play, higher = more likely
	Weight int

	// Delay (in milliseconds) for the bot to wait before sending the disconnect request
	PartDelay int

	// Buffer to store encoded PCM packets
	buffer [][]byte
}

// Array of all the sounds we have
var AIRHORN = &SoundCollection{
	Prefix: "airhorn",
	Commands: []string{
		"!airhorn",
	},
	Sounds: []*Sound{
		createSound("default", 1000, 250),
		createSound("reverb", 800, 250),
		createSound("spam", 800, 0),
		createSound("tripletap", 800, 250),
		createSound("fourtap", 800, 250),
		createSound("distant", 500, 250),
		createSound("echo", 500, 250),
		createSound("clownfull", 250, 250),
		createSound("clownshort", 250, 250),
		createSound("clownspam", 250, 0),
		createSound("highfartlong", 200, 250),
		createSound("highfartshort", 200, 250),
		createSound("midshort", 100, 250),
		createSound("truck", 10, 250),
	},
}

var KHALED = &SoundCollection{
	Prefix:    "another",
	ChainWith: AIRHORN,
	Commands: []string{
		"!anotha",
		"!anothaone",
	},
	Sounds: []*Sound{
		createSound("one", 1, 250),
		createSound("one_classic", 1, 250),
		createSound("one_echo", 1, 250),
	},
}

var CENA = &SoundCollection{
	Prefix: "jc",
	Commands: []string{
		"!johncena",
		"!cena",
	},
	Sounds: []*Sound{
		createSound("airhorn", 1, 250),
		createSound("echo", 1, 250),
		createSound("full", 1, 250),
		createSound("jc", 1, 250),
		createSound("nameis", 1, 250),
		createSound("spam", 1, 250),
	},
}

var ETHAN = &SoundCollection{
	Prefix: "ethan",
	Commands: []string{
		"!ethan",
		"!eb",
		"!ethanbradberry",
		"!h3h3",
	},
	Sounds: []*Sound{
		createSound("areyou_classic", 100, 250),
		createSound("areyou_condensed", 100, 250),
		createSound("areyou_crazy", 100, 250),
		createSound("areyou_ethan", 100, 250),
		createSound("classic", 100, 250),
		createSound("echo", 100, 250),
		createSound("high", 100, 250),
		createSound("slowandlow", 100, 250),
		createSound("cuts", 30, 250),
		createSound("beat", 30, 250),
		createSound("sodiepop", 1, 250),
	},
}

var COW = &SoundCollection{
	Prefix: "cow",
	Commands: []string{
		"!stan",
		"!stanislav",
	},
	Sounds: []*Sound{
		createSound("herd", 10, 250),
		createSound("moo", 10, 250),
		createSound("x3", 1, 250),
	},
}

var BIRTHDAY = &SoundCollection{
	Prefix: "birthday",
	Commands: []string{
		"!birthday",
		"!bday",
	},
	Sounds: []*Sound{
		createSound("horn", 50, 250),
		createSound("horn3", 30, 250),
		createSound("sadhorn", 25, 250),
		createSound("weakhorn", 25, 250),
	},
}

var WOW = &SoundCollection{
	Prefix: "wow",
	Commands: []string{
		"!wowthatscool",
		"!wtc",
	},
	Sounds: []*Sound{
		createSound("thatscool", 50, 250),
	},
}

var OKBUDDY = &SoundCollection{
	Prefix: "okbuddy",
	Commands: []string{
		"!okbuddy",
		"!okaybuddy",
		"!okb",
	},
	Sounds: []*Sound{
		createSound("1", 50, 250),
		createSound("2", 50, 250),
		createSound("3", 50, 250),
	},
}

var COLLECTIONS = []*SoundCollection{
	AIRHORN,
	KHALED,
	CENA,
	ETHAN,
	COW,
	BIRTHDAY,
	WOW,
	OKBUDDY,
}

// Create a Sound struct
func createSound(Name string, Weight int, PartDelay int) *Sound {
	return &Sound{
		Name:      Name,
		Weight:    Weight,
		PartDelay: PartDelay,
		buffer:    make([][]byte, 0),
	}
}

func (s *SoundCollection) Load() {
	for _, sound := range s.Sounds {
		s.soundRange += sound.Weight
		sound.Load(s)
	}
}

func (s *SoundCollection) Random() *Sound {
	var (
		i      int
		number = randomRange(0, s.soundRange)
	)

	for _, sound := range s.Sounds {
		i += sound.Weight

		if number < i {
			return sound
		}
	}
	return nil
}

// Load attempts to load an encoded sound file from disk
// DCA files are pre-computed sound files that are easy to send to Discord.
// If you would like to create your own DCA files, please use:
// https://github.com/nstafie/dca-rs
// eg: dca-rs --raw -i <input wav file> > <output file>
func (s *Sound) Load(c *SoundCollection) error {
	path := fmt.Sprintf("audio/%v_%v.dca", c.Prefix, s.Name)

	file, err := os.Open(path)

	if err != nil {
		fmt.Println("error opening dca file :", err)
		return err
	}

	var opuslen int16

	for {
		// read opus frame length from dca file
		err = binary.Read(file, binary.LittleEndian, &opuslen)

		// If this is the end of the file, just return
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			return nil
		}

		if err != nil {
			fmt.Println("error reading from dca file :", err)
			return err
		}

		// read encoded pcm from dca file
		InBuf := make([]byte, opuslen)
		err = binary.Read(file, binary.LittleEndian, &InBuf)

		// Should not be any end of file errors
		if err != nil {
			fmt.Println("error reading from dca file :", err)
			return err
		}

		// append encoded pcm data to the buffer
		s.buffer = append(s.buffer, InBuf)
	}
}

// Plays this sound over the specified VoiceConnection
func (s *Sound) Play(vc *discordgo.VoiceConnection) {
	vc.Speaking(true)
	defer vc.Speaking(false)

	for _, buff := range s.buffer {
		vc.OpusSend <- buff
	}
}

// Attempts to find the current users voice channel inside a given guild
func getCurrentVoiceChannel(user *discordgo.User, guild *discordgo.Guild) *discordgo.Channel {
	for _, vs := range guild.VoiceStates {
		if vs.UserID == user.ID {
			channel, _ := discord.State.Channel(vs.ChannelID)
			return channel
		}
	}
	return nil
}

// Returns a random integer between min and max
func randomRange(min, max int) int {
	rand.Seed(time.Now().UTC().UnixNano())
	return rand.Intn(max-min) + min
}

// Prepares a play
func createPlay(user *discordgo.User, guild *discordgo.Guild, coll *SoundCollection, sound *Sound) *Play {
	// Grab the users voice channel
	channel := getCurrentVoiceChannel(user, guild)
	if channel == nil {
		log.WithFields(log.Fields{
			"user":  user.ID,
			"guild": guild.ID,
		}).Warning("Failed to find channel to play sound in")
		return nil
	}

	// Create the play
	play := &Play{
		GuildID:   guild.ID,
		ChannelID: channel.ID,
		Sound:     sound,
		Forced:    true,
	}

	// If we didn't get passed a manual sound, generate a random one
	if play.Sound == nil {
		play.Sound = coll.Random()
		play.Forced = false
	}

	// If the collection is a chained one, set the next sound
	if coll.ChainWith != nil {
		play.Next = &Play{
			GuildID:   play.GuildID,
			ChannelID: play.ChannelID,
			Sound:     coll.ChainWith.Random(),
			Forced:    play.Forced,
		}
	}

	return play
}

// Prepares and enqueues a play into the ratelimit/buffer guild queue
func enqueuePlay(user *discordgo.User, guild *discordgo.Guild, coll *SoundCollection, sound *Sound) {
	play := createPlay(user, guild, coll, sound)
	if play == nil {
		return
	}

	m.Lock()
	if queue, ok := queues[guild.ID]; ok {
		if len(queue) < MAX_QUEUE_SIZE {
			queues[guild.ID] = append(queue, play)
		}
	} else {
		queues[guild.ID] = []*Play{play}
		go runPlayer(guild.ID)
	}
	m.Unlock()
}

func runPlayer(guildId string) {
	var vc *discordgo.VoiceConnection
	for {
		m.Lock()
		var play *Play

		if queue, ok := queues[guildId]; ok && len(queue) > 0 {
			play = queue[0]
			queues[guildId] = queue[1:]
		} else {
			break
		}
		m.Unlock()

		// If we need to change channels, do that now
		if vc != nil && vc.ChannelID != play.ChannelID {
			vc.ChangeChannel(play.ChannelID, false, false)
			time.Sleep(time.Millisecond * 125)
		}

		var err error
		vc, err = playSound(play, vc)
		if err != nil {
			log.Println(err)
		}
	}
	if vc != nil {
		vc.Disconnect()
	}

	delete(queues, guildId)
	m.Unlock()
}

// Play a sound
func playSound(play *Play, vc *discordgo.VoiceConnection) (*discordgo.VoiceConnection, error) {
	if vc == nil || !vc.Ready {
		var err error
		vc, err = discord.ChannelVoiceJoin(play.GuildID, play.ChannelID, false, true)
		if err != nil {
			return nil, err
		}
	}

	time.Sleep(time.Millisecond * 32)

	play.Sound.Play(vc)

	// If this is chained, play the chained sound
	if play.Next != nil {
		playSound(play.Next, vc)
	}

	time.Sleep(time.Millisecond * time.Duration(play.Sound.PartDelay))

	return vc, nil
}

func onReady(s *discordgo.Session, event *discordgo.Ready) {
	log.Info("Recieved READY payload")
	s.UpdateStatus(0, "!okbuddy")
}

func scontains(key string, options ...string) bool {
	for _, item := range options {
		if item == key {
			return true
		}
	}
	return false
}

func onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if len(m.Content) <= 0 || m.Content[0] != '!' {
		return
	}

	parts := strings.Split(strings.ToLower(m.Content), " ")

	channel, _ := discord.State.Channel(m.ChannelID)
	if channel == nil {
		log.WithFields(log.Fields{
			"channel": m.ChannelID,
			"message": m.ID,
		}).Warning("Failed to grab channel")
		return
	}

	guild, _ := discord.State.Guild(channel.GuildID)
	if guild == nil {
		log.WithFields(log.Fields{
			"guild":   channel.GuildID,
			"channel": channel,
			"message": m.ID,
		}).Warning("Failed to grab guild")
		return
	}

	// Find the collection for the command we got
	for _, coll := range COLLECTIONS {
		if scontains(parts[0], coll.Commands...) {

			// If they passed a specific sound effect, find and select that (otherwise play nothing)
			var sound *Sound
			if len(parts) > 1 {
				for _, s := range coll.Sounds {
					if parts[1] == s.Name {
						sound = s
					}
				}

				if sound == nil {
					return
				}
			}

			go enqueuePlay(m.Author, guild, coll, sound)
			return
		}
	}
}

func main() {
	var (
		Token = flag.String("t", "", "Discord Authentication Token")
		err   error
	)
	flag.Parse()

	// Preload all the sounds
	log.Info("Preloading sounds...")
	for _, coll := range COLLECTIONS {
		coll.Load()
	}

	// Create a discord session
	log.Info("Starting discord session...")
	discord, err = discordgo.New(*Token)
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Fatal("Failed to create discord session")
		return
	}

	discord.ShardCount = 1

	discord.ShouldReconnectOnError = true

	discord.AddHandler(onReady)
	discord.AddHandler(onMessageCreate)

	err = discord.Open()
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Fatal("Failed to create discord websocket connection")
		return
	}

	// We're running!
	log.Info("AIRHORNBOT is ready to horn it up.")

	// Wait for a signal to quit
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)
	<-c
	log.Info("AIRHORNBOT exiting")
}
