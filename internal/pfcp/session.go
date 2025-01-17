package pfcp

import (
	"net"

	"github.com/wmnsk/go-pfcp/ie"
	"github.com/wmnsk/go-pfcp/message"

	"github.com/free5gc/go-upf/internal/report"
)

func (s *PfcpServer) handleSessionEstablishmentRequest(
	req *message.SessionEstablishmentRequest,
	addr net.Addr,
) {
	// TODO: error response
	s.log.Infoln("handleSessionEstablishmentRequest")

	if req.NodeID == nil {
		s.log.Errorln("not found NodeID")
		return
	}
	rnodeid, err := req.NodeID.NodeID()
	if err != nil {
		s.log.Errorln(err)
		return
	}
	s.log.Debugf("remote nodeid: %v\n", rnodeid)

	rnode, ok := s.rnodes[rnodeid]
	if !ok {
		s.log.Errorf("not found NodeID %v\n", rnodeid)
		return
	}

	if req.CPFSEID == nil {
		s.log.Errorln("not found CP F-SEID")
		return
	}
	fseid, err := req.CPFSEID.FSEID()
	if err != nil {
		s.log.Errorln(err)
		return
	}
	s.log.Debugf("fseid.SEID: %#x\n", fseid.SEID)

	// allocate a session
	sess := rnode.NewSess(fseid.SEID)

	// TODO: rollback transaction
	for _, i := range req.CreateFAR {
		err = sess.CreateFAR(i)
		if err != nil {
			sess.log.Errorf("Est CreateFAR error: %+v", err)
		}
	}

	for _, i := range req.CreateQER {
		err = sess.CreateQER(i)
		if err != nil {
			sess.log.Errorf("Est CreateQER error: %+v", err)
		}
	}

	for _, i := range req.CreateURR {
		err = sess.CreateURR(i)
		if err != nil {
			sess.log.Errorf("Est CreateURR error: %+v", err)
		}
	}

	if req.CreateBAR != nil {
		err = sess.CreateBAR(req.CreateBAR)
		if err != nil {
			sess.log.Errorf("Est CreateBAR error: %+v", err)
		}
	}

	for _, i := range req.CreatePDR {
		err = sess.CreatePDR(i)
		if err != nil {
			sess.log.Errorf("Est CreatePDR error: %+v", err)
		}
	}

	var v4 net.IP
	/*############################## */
	/* node id is the wrong ip. TODO modify with n3 interface ip addr.  */

	// addrv4, err := net.ResolveIPAddr("ip4", s.nodeID)
	// if err == nil {
	// 	v4 = addrv4.IP.To4()
	// }
	/*############################## */

	addrv4, err := net.ResolveIPAddr("ip4", s.nodeID)
	if err == nil {
		v4 = addrv4.IP.To4()
	}

	// TODO: support v6
	var v6 net.IP
	var createdPDR *ie.IE
	var chooseBit uint8 = 0x4

	var rsp *message.SessionEstablishmentResponse

	for _, createPdrIE := range req.CreatePDR {

		pdi, err := createPdrIE.PDI()
		if err != nil {
			sess.log.Errorf("Cannot get pdi from createdPDR %+v", err)
		}

		for _, pdiIE := range pdi {

			switch pdiIE.Type {
			case ie.FTEID:
				fteid, err := pdiIE.FTEID()
				if err != nil {
					sess.log.Errorf("Cannot get fteid from pdi %+v", err)
				}

				chooseFlag := fteid.Flags & chooseBit
				if chooseFlag != 0 {
					pdrid, err := createPdrIE.PDRID()
					if err != nil {
						sess.log.Errorf("Cannot get PDRID: %+v", err)
					}

					createdPDR = ie.NewCreatedPDR(
						ie.NewPDRID(pdrid),
						ie.NewFTEID(0x01, 0x00000001, v4, v6, 0),
						ie.NewUEIPAddress(0x02, "10.0.0.1", "", 0, 0),
					)
					break
				}
			}
		}
	}

	if createdPDR != nil {
		rsp = message.NewSessionEstablishmentResponse(
			0,             // mp
			0,             // fo
			sess.RemoteID, // seid
			req.Header.SequenceNumber,
			0, // pri
			newIeNodeID(s.nodeID),
			ie.NewCause(ie.CauseRequestAccepted),
			ie.NewFSEID(sess.LocalID, v4, v6),
			createdPDR,
		)
	} else {
		rsp = message.NewSessionEstablishmentResponse(
			0,             // mp
			0,             // fo
			sess.RemoteID, // seid
			req.Header.SequenceNumber,
			0, // pri
			newIeNodeID(s.nodeID),
			ie.NewCause(ie.CauseRequestAccepted),
			ie.NewFSEID(sess.LocalID, v4, v6),
		)
	}

	err = s.sendRspTo(rsp, addr)
	if err != nil {
		s.log.Errorln(err)
		return
	}
}

func (s *PfcpServer) handleSessionModificationRequest(
	req *message.SessionModificationRequest,
	addr net.Addr,
) {
	// TODO: error response
	s.log.Infoln("handleSessionModificationRequest")

	sess, err := s.lnode.Sess(req.SEID())
	if err != nil {
		s.log.Errorf("handleSessionModificationRequest: %v", err)
		rsp := message.NewSessionModificationResponse(
			0, // mp
			0, // fo
			0, // seid
			req.Header.SequenceNumber,
			0, // pri
			ie.NewCause(ie.CauseSessionContextNotFound),
		)

		err = s.sendRspTo(rsp, addr)
		if err != nil {
			s.log.Errorln(err)
			return
		}
		return
	}

	if req.NodeID != nil {
		// TS 29.244 7.5.4:
		// This IE shall be present if a new SMF in an SMF Set,
		// with one PFCP association per SMF and UPF (see clause 5.22.3),
		// takes over the control of the PFCP session.
		// When present, it shall contain the unique identifier of the new SMF.
		rnodeid, err1 := req.NodeID.NodeID()
		if err1 != nil {
			s.log.Errorln(err)
			return
		}
		s.log.Debugf("new remote nodeid: %v\n", rnodeid)
		s.UpdateNodeID(sess.rnode, rnodeid)
	}

	for _, i := range req.CreateFAR {
		err = sess.CreateFAR(i)
		if err != nil {
			sess.log.Errorf("Mod CreateFAR error: %+v", err)
		}
	}

	for _, i := range req.CreateQER {
		err = sess.CreateQER(i)
		if err != nil {
			sess.log.Errorf("Mod CreateQER error: %+v", err)
		}
	}

	for _, i := range req.CreateURR {
		err = sess.CreateURR(i)
		if err != nil {
			sess.log.Errorf("Mod CreateURR error: %+v", err)
		}
	}

	if req.CreateBAR != nil {
		err = sess.CreateBAR(req.CreateBAR)
		if err != nil {
			sess.log.Errorf("Mod CreateBAR error: %+v", err)
		}
	}

	for _, i := range req.CreatePDR {
		err = sess.CreatePDR(i)
		if err != nil {
			sess.log.Errorf("Mod CreatePDR error: %+v", err)
		}
	}

	for _, i := range req.RemoveFAR {
		err = sess.RemoveFAR(i)
		if err != nil {
			sess.log.Errorf("Mod RemoveFAR error: %+v", err)
		}
	}

	for _, i := range req.RemoveQER {
		err = sess.RemoveQER(i)
		if err != nil {
			sess.log.Errorf("Mod RemoveQER error: %+v", err)
		}
	}

	var usars []report.USAReport
	for _, i := range req.RemoveURR {
		rs, err1 := sess.RemoveURR(i)
		if err1 != nil {
			sess.log.Errorf("Mod RemoveURR error: %+v", err1)
			continue
		}
		if len(rs) > 0 {
			usars = append(usars, rs...)
		}
	}

	if req.RemoveBAR != nil {
		err = sess.RemoveBAR(req.RemoveBAR)
		if err != nil {
			sess.log.Errorf("Mod RemoveBAR error: %+v", err)
		}
	}

	for _, i := range req.RemovePDR {
		rs, err1 := sess.RemovePDR(i)
		if err1 != nil {
			sess.log.Errorf("Mod RemovePDR error: %+v", err1)
		}
		if len(rs) > 0 {
			usars = append(usars, rs...)
		}
	}

	for _, i := range req.UpdateFAR {
		err = sess.UpdateFAR(i)
		if err != nil {
			sess.log.Errorf("Mod UpdateFAR error: %+v", err)
		}
	}

	for _, i := range req.UpdateQER {
		err = sess.UpdateQER(i)
		if err != nil {
			sess.log.Errorf("Mod UpdateQER error: %+v", err)
		}
	}

	for _, i := range req.UpdateURR {
		rs, err1 := sess.UpdateURR(i)
		if err1 != nil {
			sess.log.Errorf("Mod UpdateURR error: %+v", err1)
			continue
		}
		if len(rs) > 0 {
			usars = append(usars, rs...)
		}
	}

	if req.UpdateBAR != nil {
		err = sess.UpdateBAR(req.UpdateBAR)
		if err != nil {
			sess.log.Errorf("Mod UpdateBAR error: %+v", err)
		}
	}

	for _, i := range req.UpdatePDR {
		rs, err1 := sess.UpdatePDR(i)
		if err1 != nil {
			sess.log.Errorf("Mod UpdatePDR error: %+v", err1)
		}
		if len(rs) > 0 {
			usars = append(usars, rs...)
		}
	}

	for _, i := range req.QueryURR {
		rs, err1 := sess.QueryURR(i)
		if err1 != nil {
			sess.log.Errorf("Mod QueryURR error: %+v", err1)
			continue
		}
		if len(rs) > 0 {
			usars = append(usars, rs...)
		}
	}

	rsp := message.NewSessionModificationResponse(
		0,             // mp
		0,             // fo
		sess.RemoteID, // seid
		req.Header.SequenceNumber,
		0, // pri
		ie.NewCause(ie.CauseRequestAccepted),
	)
	for _, r := range usars {
		urrInfo, ok := sess.URRIDs[r.URRID]
		if !ok {
			sess.log.Warnf("Sess Mod: URRInfo[%#x] not found", r.URRID)
			continue
		}
		r.URSEQN = sess.URRSeq(r.URRID)
		rsp.UsageReport = append(rsp.UsageReport,
			ie.NewUsageReportWithinSessionModificationResponse(
				r.IEsWithinSessModRsp(
					urrInfo.MeasureMethod, urrInfo.MeasureInformation)...,
			))

		if urrInfo.removed {
			delete(sess.URRIDs, r.URRID)
		}
	}

	err = s.sendRspTo(rsp, addr)
	if err != nil {
		s.log.Errorln(err)
		return
	}
}

func (s *PfcpServer) handleSessionDeletionRequest(
	req *message.SessionDeletionRequest,
	addr net.Addr,
) {
	// TODO: error response
	s.log.Infoln("handleSessionDeletionRequest")

	lSeid := req.SEID()
	sess, err := s.lnode.Sess(lSeid)
	if err != nil {
		s.log.Errorf("handleSessionDeletionRequest: %v", err)
		rsp := message.NewSessionDeletionResponse(
			0, // mp
			0, // fo
			0, // seid
			req.Header.SequenceNumber,
			0, // pri
			ie.NewCause(ie.CauseSessionContextNotFound),
			ie.NewReportType(0, 0, 1, 0),
		)

		err = s.sendRspTo(rsp, addr)
		if err != nil {
			s.log.Errorln(err)
			return
		}
		return
	}

	usars := sess.rnode.DeleteSess(lSeid)

	rsp := message.NewSessionDeletionResponse(
		0,             // mp
		0,             // fo
		sess.RemoteID, // seid
		req.Header.SequenceNumber,
		0, // pri
		ie.NewCause(ie.CauseRequestAccepted),
	)
	for _, r := range usars {
		urrInfo, ok := sess.URRIDs[r.URRID]
		if !ok {
			sess.log.Warnf("Sess Del: URRInfo[%#x] not found", r.URRID)
			continue
		}
		r.URSEQN = sess.URRSeq(r.URRID)
		// indicates usage report being reported for a URR due to the termination of the PFCP session
		r.USARTrigger.Flags |= report.USAR_TRIG_TERMR
		rsp.UsageReport = append(rsp.UsageReport,
			ie.NewUsageReportWithinSessionDeletionResponse(
				r.IEsWithinSessDelRsp(
					urrInfo.MeasureMethod, urrInfo.MeasureInformation)...,
			))

		if urrInfo.removed {
			delete(sess.URRIDs, r.URRID)
		}
	}

	err = s.sendRspTo(rsp, addr)
	if err != nil {
		s.log.Errorln(err)
		return
	}
}

func (s *PfcpServer) handleSessionReportResponse(
	rsp *message.SessionReportResponse,
	addr net.Addr,
	req message.Message,
) {
	s.log.Infoln("handleSessionReportResponse")

	s.log.Debugf("seid: %#x\n", rsp.SEID())
	if rsp.Header.SEID == 0 {
		s.log.Warnf("rsp SEID is 0; no this session on remote; delete it on local")
		sess, err := s.lnode.RemoteSess(req.SEID(), addr)
		if err != nil {
			s.log.Errorln(err)
			return
		}
		sess.rnode.DeleteSess(sess.LocalID)
		return
	}

	sess, err := s.lnode.Sess(rsp.SEID())
	if err != nil {
		s.log.Errorln(err)
		return
	}

	s.log.Debugf("sess: %#+v\n", sess)
}

func (s *PfcpServer) handleSessionReportRequestTimeout(
	req *message.SessionReportRequest,
	addr net.Addr,
) {
	s.log.Warnf("handleSessionReportRequestTimeout: SEID[%#x]", req.SEID())
	// TODO?
}
