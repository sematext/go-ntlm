// Copyright 2013 Thomson Reuters Global Resources. BSD License please see License file for more information

package ntlm

import (
	"bytes"
	rc4P "crypto/rc4"
	"encoding/binary"
	"errors"
	"log"
	"strings"
	"time"
)

/*******************************
 Shared Session Data and Methods
*******************************/

// V2Session is the shared session data and methods for NTLMv2
type V2Session struct {
	SessionData
}

// SetUserInfo sets the username, password, and domain for the session
func (n *V2Session) SetUserInfo(username string, password string, domain string, workstation string) {
	n.user = username
	n.password = password
	n.userDomain = domain
	n.workstation = workstation
}

// GetUserInfo returns the username, password, and domain for the session
func (n *V2Session) GetUserInfo() (string, string, string, string) {
	return n.user, n.password, n.userDomain, n.workstation
}

// SetMode sets the mode for the session
func (n *V2Session) SetMode(mode Mode) {
	n.mode = mode
}

// Version returns the NTLM version of the session
func (n *V2Session) Version() int {
	return 2
}

func (n *V2Session) fetchResponseKeys() (err error) {
	// Usually at this point we'd go out to Active Directory and get these keys
	// Here we are assuming we have the information locally
	n.responseKeyLM = lmowfv2(n.user, n.password, n.userDomain)
	n.responseKeyNT = ntowfv2(n.user, n.password, n.userDomain)
	return
}

// GetSessionData returns the session data for the session
func (n *V2ServerSession) GetSessionData() *SessionData {
	return &n.SessionData
}

// Define ComputeResponse(NegFlg, ResponseKeyNT, ResponseKeyLM, CHALLENGE_MESSAGE.ServerChallenge, ClientChallenge, Time, ServerName)
// ServerNameBytes - The NtChallengeResponseFields.NTLMv2_RESPONSE.NTLMv2_CLIENT_CHALLENGE.AvPairs field structure of the AUTHENTICATE_MESSAGE payload.
func (n *V2Session) computeExpectedResponses(timestamp []byte, avPairBytes []byte) (err error) {
	temp := concat([]byte{0x01}, []byte{0x01}, zeroBytes(6), timestamp, n.clientChallenge, zeroBytes(4), avPairBytes, zeroBytes(4))
	ntProofStr := hmacMd5(n.responseKeyNT, concat(n.serverChallenge, temp))
	n.ntChallengeResponse = concat(ntProofStr, temp)
	n.lmChallengeResponse = concat(hmacMd5(n.responseKeyLM, concat(n.serverChallenge, n.clientChallenge)), n.clientChallenge)
	n.sessionBaseKey = hmacMd5(n.responseKeyNT, ntProofStr)
	return
}

func (n *V2Session) computeKeyExchangeKey() (err error) {
	n.keyExchangeKey = n.sessionBaseKey
	return
}

func (n *V2Session) calculateKeys(ntlmRevisionCurrent uint8) (err error) {
	n.ClientSigningKey = signKey(n.NegotiateFlags, n.exportedSessionKey, "Client")
	n.ServerSigningKey = signKey(n.NegotiateFlags, n.exportedSessionKey, "Server")
	n.ClientSealingKey = sealKey(n.NegotiateFlags, n.exportedSessionKey, "Client")
	n.ServerSealingKey = sealKey(n.NegotiateFlags, n.exportedSessionKey, "Server")
	return
}

func (n *V2Session) Seal(message []byte) ([]byte, error) {
	return nil, nil
}

func (n *V2Session) Sign(message []byte) ([]byte, error) {
	return nil, nil
}

// Mildly ghetto that we expose this
func NtlmVCommonMac(message []byte, sequenceNumber int, sealingKey, signingKey []byte, NegotiateFlags uint32) []byte {
	var handle *rc4P.Cipher
	// TODO: Need to keep track of the sequence number for connection oriented NTLM
	if NTLMSSP_NEGOTIATE_DATAGRAM.IsSet(NegotiateFlags) && NTLMSSP_NEGOTIATE_EXTENDED_SESSIONSECURITY.IsSet(NegotiateFlags) {
		handle, _ = reinitSealingKey(sealingKey, sequenceNumber)
	} else if NTLMSSP_NEGOTIATE_DATAGRAM.IsSet(NegotiateFlags) {
		// CONOR: Reinitializing the rc4 cipher on every requst, but not using the
		// algorithm as described in the MS-NTLM document. Just reinitialize it directly.
		handle, _ = rc4Init(sealingKey)
	}
	sig := mac(NegotiateFlags, handle, signingKey, uint32(sequenceNumber), message)
	return sig.Bytes()
}

func NtlmV2Mac(message []byte, sequenceNumber int, handle *rc4P.Cipher, sealingKey, signingKey []byte, NegotiateFlags uint32) []byte {
	// TODO: Need to keep track of the sequence number for connection oriented NTLM
	if NTLMSSP_NEGOTIATE_DATAGRAM.IsSet(NegotiateFlags) && NTLMSSP_NEGOTIATE_EXTENDED_SESSIONSECURITY.IsSet(NegotiateFlags) {
		handle, _ = reinitSealingKey(sealingKey, sequenceNumber)
	} else if NTLMSSP_NEGOTIATE_DATAGRAM.IsSet(NegotiateFlags) {
		// CONOR: Reinitializing the rc4 cipher on every requst, but not using the
		// algorithm as described in the MS-NTLM document. Just reinitialize it directly.
		handle, _ = rc4Init(sealingKey)
	}
	sig := mac(NegotiateFlags, handle, signingKey, uint32(sequenceNumber), message)
	return sig.Bytes()
}

func (n *V2ServerSession) Mac(message []byte, sequenceNumber int) ([]byte, error) {
	mac := NtlmV2Mac(message, sequenceNumber, n.serverHandle, n.ServerSealingKey, n.ServerSigningKey, n.NegotiateFlags)
	return mac, nil
}

func (n *V2ServerSession) VerifyMac(message, expectedMac []byte, sequenceNumber int) (bool, error) {
	mac := NtlmV2Mac(message, sequenceNumber, n.clientHandle, n.ClientSealingKey, n.ClientSigningKey, n.NegotiateFlags)
	return MacsEqual(mac, expectedMac), nil
}

func (n *V2ClientSession) Mac(message []byte, sequenceNumber int) ([]byte, error) {
	mac := NtlmV2Mac(message, sequenceNumber, n.clientHandle, n.ClientSealingKey, n.ClientSigningKey, n.NegotiateFlags)
	return mac, nil
}

func (n *V2ClientSession) VerifyMac(message, expectedMac []byte, sequenceNumber int) (bool, error) {
	mac := NtlmV2Mac(message, sequenceNumber, n.serverHandle, n.ServerSealingKey, n.ServerSigningKey, n.NegotiateFlags)
	return MacsEqual(mac, expectedMac), nil
}

/**************
 Server Session
**************/

type V2ServerSession struct {
	V2Session
}

func (n *V2ServerSession) SetServerChallenge(challenge []byte) {
	n.serverChallenge = challenge
}

func (n *V2ServerSession) ProcessNegotiateMessage(nm *NegotiateMessage) (err error) {
	n.negotiateMessage = nm
	return
}

func (n *V2ServerSession) GenerateChallengeMessage() (cm *ChallengeMessage, err error) {
	cm = new(ChallengeMessage)
	cm.Signature = []byte("NTLMSSP\x00")
	cm.MessageType = uint32(2)
	cm.TargetName, _ = CreateBytePayload(make([]byte, 0))

	flags := uint32(0)
	flags = NTLMSSP_NEGOTIATE_KEY_EXCH.Set(flags)
	flags = NTLMSSP_NEGOTIATE_VERSION.Set(flags)
	flags = NTLMSSP_NEGOTIATE_EXTENDED_SESSIONSECURITY.Set(flags)
	flags = NTLMSSP_NEGOTIATE_TARGET_INFO.Set(flags)
	flags = NTLMSSP_NEGOTIATE_IDENTIFY.Set(flags)
	flags = NTLMSSP_NEGOTIATE_ALWAYS_SIGN.Set(flags)
	flags = NTLMSSP_NEGOTIATE_NTLM.Set(flags)
	flags = NTLMSSP_NEGOTIATE_DATAGRAM.Set(flags)
	flags = NTLMSSP_NEGOTIATE_SIGN.Set(flags)
	flags = NTLMSSP_REQUEST_TARGET.Set(flags)
	flags = NTLMSSP_NEGOTIATE_UNICODE.Set(flags)
	flags = NTLMSSP_NEGOTIATE_128.Set(flags)

	cm.NegotiateFlags = flags

	n.serverChallenge = randomBytes(8)
	cm.ServerChallenge = n.serverChallenge
	cm.Reserved = make([]byte, 8)

	// Create the AvPairs we need
	pairs := new(AvPairs)
	pairs.AddAvPair(MsvAvNbDomainName, utf16FromString("SEMATEXT"))
	pairs.AddAvPair(MsvAvNbComputerName, utf16FromString("SYNTHETICS-HTTP-AGENT"))
	pairs.AddAvPair(MsvAvDnsDomainName, utf16FromString("sematext.com"))
	pairs.AddAvPair(MsvAvDnsComputerName, utf16FromString("synthetics-http-agent.sematext.com"))
	pairs.AddAvPair(MsvAvDnsTreeName, utf16FromString("Sematext.com"))
	pairs.AddAvPair(MsvAvEOL, make([]byte, 0))
	cm.TargetInfo = pairs
	cm.TargetInfoPayloadStruct, _ = CreateBytePayload(pairs.Bytes())

	cm.Version = &VersionStruct{ProductMajorVersion: uint8(6), ProductMinorVersion: uint8(1), ProductBuild: uint16(7601), NTLMRevisionCurrent: uint8(15)}
	return cm, nil
}

func (n *V2ServerSession) ProcessAuthenticateMessage(am *AuthenticateMessage) (err error) {
	n.authenticateMessage = am
	n.NegotiateFlags = am.NegotiateFlags
	n.clientChallenge = am.ClientChallenge()
	n.encryptedRandomSessionKey = am.EncryptedRandomSessionKey.Payload
	// Ignore the values used in SetUserInfo and use these instead from the authenticate message
	// They should always be correct (I hope)
	n.user = am.UserName.String()
	n.userDomain = am.DomainName.String()
	n.workstation = am.Workstation.String()
	log.Printf("(ProcessAuthenticateMessage)NTLM v2 User %s Domain %s Workstation %s", n.user, n.userDomain, n.workstation)

	err = n.fetchResponseKeys()
	if err != nil {
		return err
	}

	timestamp := am.NtlmV2Response.NtlmV2ClientChallenge.TimeStamp
	avPairsBytes := am.NtlmV2Response.NtlmV2ClientChallenge.AvPairs.Bytes()

	err = n.computeExpectedResponses(timestamp, avPairsBytes)
	if err != nil {
		return err
	}

	if !bytes.Equal(am.NtChallengeResponseFields.Payload, n.ntChallengeResponse) {
		if !bytes.Equal(am.LmChallengeResponse.Payload, n.lmChallengeResponse) {
			return errors.New("Could not authenticate")
		}
	}

	err = n.computeKeyExchangeKey()
	if err != nil {
		return err
	}

	n.mic = am.Mic
	am.Mic = zeroBytes(16)

	err = n.computeExportedSessionKey()
	if err != nil {
		return err
	}

	if am.Version == nil {
		// UGH not entirely sure how this could possibly happen, going to put this in for now
		// TODO investigate if this ever is really happening
		am.Version = &VersionStruct{ProductMajorVersion: uint8(6), ProductMinorVersion: uint8(1), ProductBuild: uint16(7601), NTLMRevisionCurrent: uint8(15)}

		log.Printf("Nil version in ntlmv2")
	}

	err = n.calculateKeys(am.Version.NTLMRevisionCurrent)
	if err != nil {
		return err
	}

	n.clientHandle, err = rc4Init(n.ClientSealingKey)
	if err != nil {
		return err
	}
	n.serverHandle, err = rc4Init(n.ServerSealingKey)
	if err != nil {
		return err
	}

	return nil
}

func (n *V2ServerSession) computeExportedSessionKey() (err error) {
	if NTLMSSP_NEGOTIATE_KEY_EXCH.IsSet(n.NegotiateFlags) {
		n.exportedSessionKey, err = rc4K(n.keyExchangeKey, n.encryptedRandomSessionKey)
		if err != nil {
			return err
		}
		// TODO: Calculate mic correctly. This calculation is not producing the right results now
		// n.calculatedMic = HmacMd5(n.exportedSessionKey, concat(n.challengeMessage.Payload, n.authenticateMessage.Bytes))
	} else {
		n.exportedSessionKey = n.keyExchangeKey
		// TODO: Calculate mic correctly. This calculation is not producing the right results now
		// n.calculatedMic = HmacMd5(n.keyExchangeKey, concat(n.challengeMessage.Payload, n.authenticateMessage.Bytes))
	}
	return nil
}

/*************
 Client Session
**************/

type V2ClientSession struct {
	V2Session
}

func (n *V2ClientSession) GenerateNegotiateMessage() (nm *NegotiateMessage, err error) {
	return nil, nil
}

func (n *V2ClientSession) ProcessChallengeMessage(cm *ChallengeMessage) (err error) {
	n.challengeMessage = cm
	n.serverChallenge = cm.ServerChallenge
	n.clientChallenge = randomBytes(8)

	n.NegotiateFlags = cm.NegotiateFlags

	err = n.fetchResponseKeys()
	if err != nil {
		return err
	}

	var payload []byte
	if NTLMSSP_NEGOTIATE_TARGET_INFO.IsSet(cm.NegotiateFlags) {
		payload = cm.TargetInfoPayloadStruct.Payload
	}
	timestamp := timeToWindowsFileTime(time.Now())
	err = n.computeExpectedResponses(timestamp, payload)
	if err != nil {
		return err
	}

	err = n.computeKeyExchangeKey()
	if err != nil {
		return err
	}

	err = n.computeEncryptedSessionKey()
	if err != nil {
		return err
	}

	ntlmRevision := uint8(0)
	if cm.Version != nil {
		ntlmRevision = cm.Version.NTLMRevisionCurrent
	}

	err = n.calculateKeys(ntlmRevision)
	if err != nil {
		return err
	}

	if len(n.ClientSigningKey) > 0 {
		n.clientHandle, err = rc4Init(n.ClientSealingKey)
		if err != nil {
			return err
		}
	}

	if len(n.ServerSealingKey) > 0 {
		n.serverHandle, err = rc4Init(n.ServerSealingKey)
		if err != nil {
			return err
		}
	}

	return nil
}

func (n *V2ClientSession) GenerateAuthenticateMessage() (am *AuthenticateMessage, err error) {
	am = new(AuthenticateMessage)
	am.Signature = []byte("NTLMSSP\x00")
	am.MessageType = uint32(3)
	am.LmChallengeResponse, _ = CreateBytePayload(n.lmChallengeResponse)
	am.NtChallengeResponseFields, _ = CreateBytePayload(n.ntChallengeResponse)
	am.DomainName, _ = CreateStringPayload(n.userDomain)
	am.UserName, _ = CreateStringPayload(n.user)
	am.Workstation, _ = CreateStringPayload(n.workstation)
	am.EncryptedRandomSessionKey, _ = CreateBytePayload(n.encryptedRandomSessionKey)
	am.NegotiateFlags = n.NegotiateFlags
	am.Mic = make([]byte, 16)
	am.Version = &VersionStruct{ProductMajorVersion: uint8(6), ProductMinorVersion: uint8(1), ProductBuild: uint16(7601), NTLMRevisionCurrent: 0x0F}
	return am, nil
}

func (n *V2ClientSession) computeEncryptedSessionKey() (err error) {
	if NTLMSSP_NEGOTIATE_KEY_EXCH.IsSet(n.NegotiateFlags) {
		n.exportedSessionKey = randomBytes(16)
		n.encryptedRandomSessionKey, err = rc4K(n.keyExchangeKey, n.exportedSessionKey)
		if err != nil {
			return err
		}
	} else {
		n.encryptedRandomSessionKey = n.keyExchangeKey
	}
	return nil
}

/********************************
 NTLM V2 Password hash functions
*********************************/

// Define ntowfv2(Passwd, User, UserDom) as
func ntowfv2(user string, passwd string, userDom string) []byte {
	concat := utf16FromString(strings.ToUpper(user) + userDom)
	return hmacMd5(md4(utf16FromString(passwd)), concat)
}

// Define lmowfv2(Passwd, User, UserDom) as
func lmowfv2(user string, passwd string, userDom string) []byte {
	return ntowfv2(user, passwd, userDom)
}

/********************************
 Helper functions
*********************************/

func timeToWindowsFileTime(t time.Time) []byte {
	var ll int64
	ll = (int64(t.Unix()) * int64(10000000)) + int64(116444736000000000)
	buffer := bytes.NewBuffer(make([]byte, 0, 8))
	binary.Write(buffer, binary.LittleEndian, ll)
	return buffer.Bytes()
}
