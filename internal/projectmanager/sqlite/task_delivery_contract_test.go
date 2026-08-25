package sqlite

import (
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"testing"
)

func TestTaskRepoDeliveryContractRoundTripAndLegacyDefault(t *testing.T) {
	ctx, _, _, _, tr, _, _, _ := setup(t)
	for _, tc := range []struct {
		id        string
		contract  pm.DeliveryContract
		effective pm.DeliveryContract
	}{
		{"TDC1", pm.DeliveryEvidenceOnly, pm.DeliveryEvidenceOnly},
		{"TDC2", "", pm.DeliveryCodeChange},
		{"TDC3", pm.DeliverySupervisorInline, pm.DeliverySupervisorInline},
	} {
		tk, err := pm.NewTask(pm.NewTaskInput{ID: pm.TaskID(tc.id), ProjectID: "P1", Title: "t", CreatedBy: "user:a", CreatedAt: t0, DeliveryContract: tc.contract})
		if err != nil {
			t.Fatal(err)
		}
		if err := tr.Save(ctx, tk); err != nil {
			t.Fatal(err)
		}
		got, err := tr.FindByID(ctx, pm.TaskID(tc.id))
		if err != nil {
			t.Fatal(err)
		}
		if got.DeliveryContract().Effective() != tc.effective {
			t.Fatalf("%s contract=%q effective=%q", tc.id, got.DeliveryContract(), got.DeliveryContract().Effective())
		}
	}
}
