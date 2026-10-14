package durable

import (
	"context"
	"testing"
	"time"
)

func TestCodemode120QuickJSDiscoverySnapshotGlobals(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	start := CodemodeStart{StartedAt: 1000, Seed: 42, Tools: []CodemodeToolDescription{{Name: "mcp.leaf", Identifier: "mcp_leaf", Description: "Search files", InputSchema: JSON{"type": "object"}, OutputSchema: JSON{"type": "string"}}}, Namespaces: []CodemodeNamespace{{Name: "file-tools", Prefix: "mcp.", Description: "Files"}}}
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	vm, err := sandbox.Start(ctx, `const found=await searchTools("files",{limit:1,namespace:"file_tools"});if(found.length!==1||found[0].name!=="mcp_leaf")throw Error("search");const described=await describeTool("mcp.leaf");if(!described.includes("Promise<string>"))throw Error("schema");const ns=await describeNamespace("file_tools");if(ns.tools[0]!=="mcp_leaf")throw Error("namespace");let denied=false;try{await describeTool("missing")}catch(e){denied=true}if(!denied)throw Error("missing allowed");for(const args of [["files",null],["files",{limit:null}],["files",{limit:1.5}],["files",{namespace:null}],[null]]){let rejected=false;try{await searchTools(...args)}catch(e){rejected=true}if(!rejected)throw Error("invalid options admitted")}return "discovered";`, start)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	var event CodemodeEvent
	for {
		event, err = vm.Next(ctx)
		if err != nil || event.Result != nil {
			break
		}
		if event.Call == nil || !event.Call.Global {
			t.Fatal("invalid discovery event", event)
		}
		value, callErr := codemodeDiscoveryOutcome(start, *event.Call)
		delivery := CodemodeDelivery{Seq: event.Call.ID, Now: 1000, Call: *event.Call}
		if callErr != nil {
			delivery.Error = callErr.Error()
		} else {
			delivery.Value = value
			delivery.HasValue = true
		}
		if err = vm.Deliver(ctx, delivery); err != nil {
			t.Fatal(err)
		}
	}
	if err != nil || event.Result == nil || event.Result.Error != "" || event.Result.Output != "discovered" {
		t.Fatal(event.Result, err)
	}
}
