package extract

import "testing"

func baseReq() PlanRequest {
	return PlanRequest{
		ParentKey: "PARENT1",
		PDFKey:    "PDF1",
		PDFName:   "paper.pdf",
		PDFHash:   "abc123",
	}
}

func TestPlanExtract_NoExisting_Create(t *testing.T) {
	t.Parallel()
	p := PlanExtract(baseReq(), false)
	if p.Action != ActionCreate {
		t.Errorf("action = %v, want ActionCreate", p.Action)
	}
}

func TestPlanExtract_HasExisting_Skip(t *testing.T) {
	t.Parallel()
	p := PlanExtract(baseReq(), true)
	if p.Action != ActionSkip {
		t.Errorf("action = %v, want ActionSkip", p.Action)
	}
}

func TestPlanExtract_HasExisting_ForceCreates(t *testing.T) {
	t.Parallel()
	req := baseReq()
	req.Force = true
	p := PlanExtract(req, true)
	if p.Action != ActionCreate {
		t.Errorf("action = %v, want ActionCreate (force should override)", p.Action)
	}
}

func TestPlanExtract_NoExisting_ForceCreates(t *testing.T) {
	t.Parallel()
	req := baseReq()
	req.Force = true
	p := PlanExtract(req, false)
	if p.Action != ActionCreate {
		t.Errorf("action = %v, want ActionCreate", p.Action)
	}
}
