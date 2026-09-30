package main

import (
	"strings"
	"testing"
)

// 行操作按钮顺序：签到 → 任务 → 余额 → 启用/禁用；国际账号的签到与任务都禁用。
func TestAccountRowButtonsOrderAndIntlDisabled(t *testing.T) {
	resetState()
	cn := renderAccountRow(workBuddyAccount{UID: "u-cn", AuthIndex: "i-cn", Variant: string(variantCn)})
	iTask, iQuota := strings.Index(cn, `data-row-action="tasks"`), strings.Index(cn, `data-row-action="quota"`)
	if iTask < 0 || iQuota < 0 || iTask > iQuota {
		t.Fatalf("任务按钮应在余额之前：tasks=%d quota=%d", iTask, iQuota)
	}

	ai := renderAccountRow(workBuddyAccount{UID: "u-ai", AuthIndex: "i-ai", Variant: string(variantAi)})
	if strings.Contains(ai, `data-row-action="tasks"`) || strings.Contains(ai, `data-row-action="checkin"`) {
		t.Error("国际账号不应提供可点击的签到/任务按钮")
	}
	if !strings.Contains(ai, `title="国际版无任务功能"`) {
		t.Error("国际账号的任务按钮应禁用并说明原因")
	}
	if !strings.Contains(ai, `data-row-action="quota"`) {
		t.Error("国际账号的余额按钮应保持可用")
	}
	if strings.Index(ai, ">任务<") > strings.Index(ai, ">余额<") {
		t.Error("国际账号行也应是任务在余额之前")
	}
}
