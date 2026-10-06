// Copyright (c) 2026 WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package emailer

// responsiveHead is shared by every email body. The inline styles stay the
// baseline, because many clients (desktop Outlook, some webmail) drop <style>
// blocks; clients that do honour them get the small-screen rules below, which
// stack each table row into a labelled block instead of squeezing its columns.
// The em-lbl spans are hidden inline so a client that ignores this block never
// shows the labels twice.
const responsiveHead = `<head>
<meta http-equiv="Content-Type" content="text/html; charset=UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<style>
@media only screen and (max-width:600px){
.em-outer{padding:8px 4px !important}
.em-pad{padding-left:14px !important;padding-right:14px !important}
.em-rows{table-layout:auto !important}
.em-rows .em-hdr{display:none !important}
.em-rows tr{display:block !important;padding:10px 0 !important;border-bottom:1px solid #e1e4e8 !important}
.em-rows td{display:block !important;width:auto !important;padding:2px 0 !important;border-bottom:0 !important;white-space:normal !important}
.em-lbl{display:inline !important;color:#57606a !important;font-weight:normal !important}
.em-kv tr{display:block !important;padding:6px 0 !important}
.em-kv td{display:block !important;width:auto !important;padding:1px 0 !important}
}
</style>
</head>
`
