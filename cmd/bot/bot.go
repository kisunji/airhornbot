package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// Handlers here need to conform exactly to func type -- even with unused vars
var _ func(*discordgo.Session, *discordgo.Ready) = onReady
var _ func(*discordgo.Session, *discordgo.MessageCreate) = onMessageCreate

var (
	// discordgo session
	discord *discordgo.Session

	// Map of Guild id's to *Play channels,
	// used for queuing and rate-limiting guilds
	queues = make(map[string][]*Play)

	MAX_QUEUE_SIZE = 6

	m sync.Mutex
)

// Play represents an individual use of the !airhorn command
type Play struct {
	GuildID   string
	ChannelID string
	Sound     *Sound

	// The next play to occur after this,
	// only used for chaining sounds like anotha
	Next *Play
}

func main() {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		fmt.Println("BOT_TOKEN environment variable not found")
		return
	}

	// Preload all the sounds
	fmt.Println("Preloading sounds...")
	for _, coll := range COLLECTIONS {
		coll.Load()
	}

	// Create a discord session
	fmt.Println("Starting discord session...")
	var err error
	discord, err = discordgo.New(token)
	if err != nil {
		fmt.Printf("failed to create discord session: %v\n", err)
		return
	}

	discord.ShardCount = 1
	discord.ShouldReconnectOnError = true
	discord.AddHandler(onReady)
	discord.AddHandler(onMessageCreate)

	err = discord.Open()
	if err != nil {
		fmt.Printf("failed to create discord websocket connection: %v\n",
			err)
		return
	}

	// We're running!
	fmt.Println("AIRHORNBOT is ready")

	// Wait for a signal to quit
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)
	<-c
	fmt.Println("AIRHORNBOT exiting")
}

func onReady(s *discordgo.Session, event *discordgo.Ready) {
	fmt.Println("Recieved READY payload")
	_ = s.UpdateStatus(0, "!okbuddy")
}

func onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if len(m.Content) <= 0 || m.Content[0] != '!' {
		return
	}

	parts := strings.Split(strings.ToLower(m.Content), " ")

	channel, _ := discord.State.Channel(m.ChannelID)
	if channel == nil {
		fmt.Printf("Failed to grab channel %v, message %v\n",
			m.ChannelID, m.ID)
		return
	}

	guild, _ := discord.State.Guild(channel.GuildID)
	if guild == nil {
		fmt.Printf("Failed to grab guild %v, channel %v, message %v\n",
			channel.GuildID, m.ChannelID, m.ID)
		return
	}

	// Find the collection for the command we got
	for _, coll := range COLLECTIONS {
		if scontains(parts[0], coll.Commands...) {

			// If they passed a specific sound effect,
			// find and select that (otherwise play nothing)
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

// Prepares and enqueues a play into the ratelimit/buffer guild queue
func enqueuePlay(user *discordgo.User, guild *discordgo.Guild, coll *SoundCollection, sound *Sound) {
	// If we didn't get passed a manual sound, generate a random one
	if sound == nil {
		sound = coll.Random()
	}
	play := createPlay(user, guild, coll, sound)
	if play == nil {
		return
	}

	m.Lock()
	if queue, ok := queues[guild.ID]; ok {
		log.Printf("queue size : %v", len(queue))
		if len(queue) < MAX_QUEUE_SIZE {
			queues[guild.ID] = append(queue, play)
		}
	} else {
		queues[guild.ID] = []*Play{play}
		go runPlayer(guild.ID)
	}
	m.Unlock()
}

// Prepares a play
func createPlay(user *discordgo.User, guild *discordgo.Guild, coll *SoundCollection, sound *Sound) *Play {
	// Grab the users voice channel
	channel := getCurrentVoiceChannel(user, guild)
	if channel == nil {
		return nil
	}
	if channel.ID == guild.AfkChannelID {
		return nil
	}

	// Create the play
	play := &Play{
		GuildID:   guild.ID,
		ChannelID: channel.ID,
		Sound:     sound,
	}

	// If the collection is a chained one, set the next sound
	if coll.ChainWith != nil {
		play.Next = &Play{
			GuildID:   play.GuildID,
			ChannelID: play.ChannelID,
			Sound:     coll.ChainWith.Random(),
		}
	}

	return play
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

func runPlayer(guildId string) {
	var vc *discordgo.VoiceConnection

	for {
		m.Lock()
		var play *Play

		if queue, ok := queues[guildId]; ok && len(queue) > 0 {
			play = queue[0]
			queues[guildId] = queue[1:]
		} else {
			m.Unlock()
			break
		}
		m.Unlock()

		// If we need to change channels, do that now
		if vc != nil && vc.ChannelID != play.ChannelID {
			_ = vc.ChangeChannel(play.ChannelID, false, false)
			time.Sleep(time.Millisecond * 125)
		}

		var err error
		vc, err = playSound(play, vc)
		if err != nil {
			fmt.Println(err)
			return
		}
	}
	if vc != nil {
		_ = vc.Disconnect()
	}

	delete(queues, guildId)
}

// Play a sound
func playSound(play *Play, vc *discordgo.VoiceConnection) (*discordgo.VoiceConnection, error) {
	if vc == nil {
		var err error
		vc, err = discord.ChannelVoiceJoin(play.GuildID, play.ChannelID, false, true)
		if err != nil {
			return vc, err
		}
	}

	play.Sound.Play(vc)

	// If this is chained, play the chained sound
	if play.Next != nil {
		_, _ = playSound(play.Next, vc)
	}

	time.Sleep(time.Millisecond * time.Duration(play.Sound.PartDelay))

	return vc, nil
}

func scontains(key string, options ...string) bool {
	for _, item := range options {
		if item == key {
			return true
		}
	}
	return false
}
