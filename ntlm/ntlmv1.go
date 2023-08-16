// Copyright 2013 Thomson Reuters Global Resources. BSD License please see License file for more information

package ntlm

import (
	"bytes"
	rc4P "crypto/rc4"
	"errors"
	"log"
	"strings"
)

/*******************************
 Shared Session Data and Methods
*******************************/

// V1Session is the shared data and methods for NTLMv1
type V1Session struct {
	SessionData
}

// SetUserInfo sets the username, password, domain, and workstation for the session
func (n *V1Session) SetUserInfo(username string, password string, domain string, workstation string) {
	n.user = username
	n.password = password
	n.userDomain = domain
	n.workstation = workstation
}

// GetUserInfo returns the username, password, domain and workstation for the session
func (n *V1Session) GetUserInfo() (string, string, string, string) {
	return n.user, n.password, n.userDomain, n.workstation
}

func (n *V1Session) SetMode(mode Mode) {
	n.mode = mode
}

func (n *V1Session) Version() int {
	return 1
}

func (n *V1Session) fetchResponseKeys() (err error) {
	n.responseKeyLM, err = lmowfv1(n.password)
	if err != nil {
		return err
	}
	n.responseKeyNT = ntowfv1(n.password)
	return
}

func (n *V1Session) computeExpectedResponses() (err error) {
	if NTLMSSP_NEGOTIATE_EXTENDED_SESSIONSECURITY.IsSet(n.NegotiateFlags) {
		n.ntChallengeResponse, err = desL(n.responseKeyNT, md5(concat(n.serverChallenge, n.clientChallenge))[0:8])
		if err != nil {
			return err
		}
		n.lmChallengeResponse = concat(n.clientChallenge, make([]byte, 16))
	} else {
		n.ntChallengeResponse, err = desL(n.responseKeyNT, n.serverChallenge)
		if err != nil {
			return err
		}
		// NoLMResponseNTLMv1: A Boolean setting that controls using the NTLM response for the LM
		// response to the server challenge when NTLMv1 authentication is used.<30>
		// <30> Section 3.1.1.1: The default value of this state variable is TRUE. Windows NT Server 4.0 SP3
		// does not support providing NTLM instead of LM responses.
		noLmResponseNtlmV1 := false
		if noLmResponseNtlmV1 {
			n.lmChallengeResponse = n.ntChallengeResponse
		} else {
			n.lmChallengeResponse, err = desL(n.responseKeyLM, n.serverChallenge)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (n *V1Session) computeSessionBaseKey() (err error) {
	n.sessionBaseKey = md4(n.responseKeyNT)
	return
}

func (n *V1Session) computeKeyExchangeKey() (err error) {
	if NTLMSSP_NEGOTIATE_EXTENDED_SESSIONSECURITY.IsSet(n.NegotiateFlags) {
		n.keyExchangeKey = hmacMd5(n.sessionBaseKey, concat(n.serverChallenge, n.lmChallengeResponse[0:8]))
	} else {
		n.keyExchangeKey, err = kxKey(n.NegotiateFlags, n.sessionBaseKey, n.lmChallengeResponse, n.serverChallenge, n.responseKeyLM)
	}
	return
}

func (n *V1Session) calculateKeys(ntlmRevisionCurrent uint8) (err error) {
	n.ClientSigningKey = signKey(n.NegotiateFlags, n.exportedSessionKey, "Client")
	n.ServerSigningKey = signKey(n.NegotiateFlags, n.exportedSessionKey, "Server")
	n.ClientSealingKey = sealKey(n.NegotiateFlags, n.exportedSessionKey, "Client")
	n.ServerSealingKey = sealKey(n.NegotiateFlags, n.exportedSessionKey, "Server")
	return
}

func (n *V1Session) Seal(message []byte) ([]byte, error) {
	return nil, nil
}

func (n *V1Session) Sign(message []byte) ([]byte, error) {
	return nil, nil
}

func ntlmV1Mac(message []byte, sequenceNumber int, handle *rc4P.Cipher, sealingKey, signingKey []byte, NegotiateFlags uint32) []byte {
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

func (n *V1ServerSession) Mac(message []byte, sequenceNumber int) ([]byte, error) {
	mac := ntlmV1Mac(message, sequenceNumber, n.serverHandle, n.ServerSealingKey, n.ServerSigningKey, n.NegotiateFlags)
	return mac, nil
}

func (n *V1ClientSession) Mac(message []byte, sequenceNumber int) ([]byte, error) {
	mac := ntlmV1Mac(message, sequenceNumber, n.clientHandle, n.ClientSealingKey, n.ClientSigningKey, n.NegotiateFlags)
	return mac, nil
}

func (n *V1ServerSession) VerifyMac(message, expectedMac []byte, sequenceNumber int) (bool, error) {
	mac := ntlmV1Mac(message, sequenceNumber, n.clientHandle, n.ClientSealingKey, n.ClientSigningKey, n.NegotiateFlags)
	return MacsEqual(mac, expectedMac), nil
}

func (n *V1ClientSession) VerifyMac(message, expectedMac []byte, sequenceNumber int) (bool, error) {
	mac := ntlmV1Mac(message, sequenceNumber, n.serverHandle, n.ServerSealingKey, n.ServerSigningKey, n.NegotiateFlags)
	return MacsEqual(mac, expectedMac), nil
}

/**************
 Server Session
**************/

type V1ServerSession struct {
	V1Session
}

func (n *V1ServerSession) ProcessNegotiateMessage(nm *NegotiateMessage) (err error) {
	n.negotiateMessage = nm
	return
}

func (n *V1ServerSession) GenerateChallengeMessage() (cm *ChallengeMessage, err error) {
	// TODO: Generate this challenge message
	return
}

func (n *V1ServerSession) SetServerChallenge(challenge []byte) {
	n.serverChallenge = challenge
}

func (n *V1ServerSession) GetSessionData() *SessionData {
	return &n.SessionData
}

func (n *V1ServerSession) ProcessAuthenticateMessage(am *AuthenticateMessage) (err error) {
	n.authenticateMessage = am
	n.NegotiateFlags = am.NegotiateFlags
	n.clientChallenge = am.ClientChallenge()
	n.encryptedRandomSessionKey = am.EncryptedRandomSessionKey.Payload
	// Ignore the values used in SetUserInfo and use these instead from the authenticate message
	// They should always be correct (I hope)
	n.user = am.UserName.String()
	n.userDomain = am.DomainName.String()
	log.Printf("(ProcessAuthenticateMessage)NTLM v1 User %s Domain %s", n.user, n.userDomain)

	err = n.fetchResponseKeys()
	if err != nil {
		return err
	}

	err = n.computeExpectedResponses()
	if err != nil {
		return err
	}

	err = n.computeSessionBaseKey()
	if err != nil {
		return err
	}

	err = n.computeKeyExchangeKey()
	if err != nil {
		return err
	}

	if !bytes.Equal(am.NtChallengeResponseFields.Payload, n.ntChallengeResponse) {
		// There is a bug with the steps in MS-NLMP. In section 3.2.5.1.2 it says you should fall through
		// to compare the lmChallengeResponse if the ntChallengeRepsonse fails, but with extended session security
		// this would *always* pass because the lmChallengeResponse and expectedLmChallengeRepsonse will always
		// be the same
		if !bytes.Equal(am.LmChallengeResponse.Payload, n.lmChallengeResponse) || NTLMSSP_NEGOTIATE_EXTENDED_SESSIONSECURITY.IsSet(n.NegotiateFlags) {
			return errors.New("Could not authenticate")
		}
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
		log.Printf("Nil version in ntlmv1")
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

func (n *V1ServerSession) computeExportedSessionKey() (err error) {
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

type V1ClientSession struct {
	V1Session
}

func (n *V1ClientSession) GenerateNegotiateMessage() (nm *NegotiateMessage, err error) {
	return nil, nil
}

func (n *V1ClientSession) ProcessChallengeMessage(cm *ChallengeMessage) (err error) {
	n.challengeMessage = cm
	n.serverChallenge = cm.ServerChallenge
	n.clientChallenge = randomBytes(8)

	n.NegotiateFlags = cm.NegotiateFlags

	err = n.fetchResponseKeys()
	if err != nil {
		return err
	}

	err = n.computeExpectedResponses()
	if err != nil {
		return err
	}

	err = n.computeSessionBaseKey()
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

func (n *V1ClientSession) GenerateAuthenticateMessage() (am *AuthenticateMessage, err error) {
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
	am.Version = &VersionStruct{ProductMajorVersion: uint8(6), ProductMinorVersion: uint8(1), ProductBuild: uint16(7601), NTLMRevisionCurrent: uint8(15)}
	return am, nil
}

func (n *V1ClientSession) computeEncryptedSessionKey() (err error) {
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
 NTLM V1 Password hash functions
*********************************/

func ntowfv1(passwd string) []byte {
	return md4(utf16FromString(passwd))
}

// ConcatenationOf( DES( UpperCase( Passwd)[0..6],"KGS!@#$%"), DES( UpperCase( Passwd)[7..13],"KGS!@#$%"))
func lmowfv1(passwd string) ([]byte, error) {
	asciiPassword := []byte(strings.ToUpper(passwd))
	keyBytes := zeroPaddedBytes(asciiPassword, 0, 14)

	first, err := des(keyBytes[0:7], []byte("KGS!@#$%"))
	if err != nil {
		return nil, err
	}
	second, err := des(keyBytes[7:14], []byte("KGS!@#$%"))
	if err != nil {
		return nil, err
	}

	return append(first, second...), nil
}
