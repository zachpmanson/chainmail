# M1 shared restricted DAILY-parts v2 — reviewed exact byte contract

Pure M1 authoring approved by independent review7e491c9e; title-free parse and BOTH UTC/local-year witness bounds clarified from its notes. Parent's literal fixture set now8accepts/16members/10rejects SHA256 `11070c2e368589a042dfb64ba553430d23606a300f60258f6e8de04da83a41a0`: the seven reviewed cases and all rejections are unchanged; one explicitly authored minute00 singleton added. Standard formatter made explicit and half-fixture helper restricted to literal phase17/47. This is not M2/writer/rollout approval.

Owner-approved restricted new-hourly scope; fullM0AFAIL unchanged. M0B actualstart-timegatePASS+independent result approvala09acd78. This M1 milestone is PUREcompiler/codec/membership predicate/tests ONLY; no currentcreate/default/reader/UI/gateway/API/journal/legacy path integration or live/deploy. Parent freezes shared literal fixtures before two app writers. Native/web must agree exact bytes; no compiler-generated goldens. M2 full authority/recovery contract still separate.

## Logical input/admission

Input `Schedule` is existing title/start/timeZone/cadence/interval/until/workHours. Run existing validation unchanged: title bounded500 UTF8 bytes/noCRLF/nonblank, original start/end exact Melbourne minute1970..2100/unambiguous, timezoneAustralia/Melbourne, hourly intervalin9supported, work startweekday09<=hour<17, untilSTRICTLY>start. M1 compile requires cadencehourly. Title stays OUTSIDE canonical payload/description/hash. Metadata parsing returns TITLE-FREE logical metadata; it may use constant valid title `Synthetic` solely to re-run existing timing validation, never reconstruct the owner's title or copy title into payload.

Potential hour grid before UNTIL: forhour0..23, keepwork09..16ifwork; ifinterval>=1 keepfloorMod(hour-globalStart.hour,intervalHours)==0, halfkeepsallworkhours. IfANYpotentialhour==2 rejectWHOLEinput. Neverdrop2, never usefiniteendexception/fallbackBYHOUR. Thus plain0.5/1alwaysreject, plainotherphasesonlysafeones; all9workcadencespossible. Wholeminute=[globalMinute];half=[m%30,m%30+30]. Hours ascending, atmostplain12/work8, generichard24. Originalcadence represented integerintervalMinutes {30,60,120,180,240,360,480,720,1440}.

For eachpotentialhour derivefirstmatchingdate/minute>=globalStartandvalidworkweekday, strictoneoffset. Until compareabsoluteUTC: iffirst>untilOMITmember, equalityINCLUDE. Forincludedderivedanchors enforceyear1970..2100; ifrequiredanchoroutside supportedrange REJECTinput(no drop). Derive/rangeconvert usingjava.time/Go withoutnormalization todecideUNTILomission beforeincludedanchor-yearcheck; finiterepeatendingbefore2101 mayomit2101anchors, unboundedrequired2101anchorrejects. Input==end negative. Everyincludedanchor mustmatchrule andglobaltimeline, no earlierhalf-minute phantom. Maxoneweekdayskipsearch<=8days. Rules calculateend=startEpoch+300000 elapsedms, even01:59 acrossspring->03:04; duration=PT5M. No recurrenceprovider proof here.

## IDs

`task` and `incarnation` supplied separately ascanonicalLOWERCASERFC4122UUIDstrings: exact8-4-4-4-12hex/version1..8/variant8,9,a,b. No uppercase/whitespace/normalization. M2generatesrandomnewtask+incarnationbeforeadmittedcreate. No imported task migration or newincarnation timingedit firstversion. They need notdiffer forgrammar; functionsMUSTnottreatUUID/hashasmutationpermission.

For each rolehourHH (two decimaldigits), immutable `memberID` is64lowerhexSHA256 over EXACTASCIIUTF8:
`hassles-daily-part-member/2\n` +task+`\n`+incarnation+`\n`+HH+`\n`.
This is logical manifest identity, NOT actualICSUID/href/native_id/uid2445. No assumptionNativecanwrite/controluid2445. Physicalidentities separatelycaptured later; creationwebmaychooseownreservedhref/UID butsharedvaliditydoesnotrequiremetadataID==physicalUID. No guessedretarget/replacement.

## Canonical payload exactASCII

Alltimes inpayload `yyyy-MM-dd'T'HH:mm` (16ASCIIbytes), timezonefixedstring. Work0/1. Until '-' ifabsentelseexactoriginalvalidcivilend. No arbitrarystrings/title/notes/extensions/space/tabs/CR/Unicode. FinalLFmandatory. Fields EXACTORDER, noextras/duplicates:

hassles-daily-parts/2\n
task=<uuid>\n
incarnation=<uuid>\n
timezone=Australia/Melbourne\n
start=<globalLocal>\n
minutes=<canonicalbase10integer>\n
work=<0or1>\n
until=<-orlocal>\n
member=<HH>,<firstLocal>,<memberID64>\n
... eachincludedhourascending exactlyonce

The fenced-lookingtextaboveis explanatory: canonicaloutputisconsecutivelines withNOemptylines. EverymemberlineendsLF. Noemptyroster (originaleligiblefirstslotexistswhenend>start). `planHash`=64lowerhexSHA256exactpayloadUTF8bytes. Payload includesanchors/complete memberroster, doesNOTinclude mutabletitles/EXDATE/timestamps/physicalIDs. Rules/minutes/durationderivedbythisversionratherthanarbitrarypayloadstrings. Parsing muststrictlyparseboundedtakes thenINDEPENDENTLYRECOMPILEfromlogicalinputandIDs, andrequirebyte-for-bytecanonicalpayload==input plushashmatch; no trustingmember-suppliedroster/hash. DecodeinvalidUTF8/nonASCII/malformed/unknown-version/duplicate/missingfields/noncanonicalinteger/order/UUID/role/hash/capinvalid→unsupported/error, no fallback tolegacy. Metadatahashdoesnotauthorizewrites.

## Exact description / closed markers

Effective decoded VEVENT DESCRIPTION/nativeeventdescription EXACTLY3LF-separatedlines, NOTRAILINGLF:
`[hassle:v2 task=<uuid> incarnation=<uuid> member=<memberID64> plan=<planHash64>]`
`[chainmail:hourly:v2 minutes=<integer>]`
`[hassle:parts:v2 data=<payloadBase64UrlNoPadding>]`

Optionalexactfourthline `[chainmail:completed-instances:v1]` onlywhencompletionmetadataisintended; parserreturnsprovenanceflag, rawEXDATEvalidationlaterrequiresmarkeriffnonemptypercurrentpolicy. First3linesbyteidenticalasidefrommemberIDpermember. Pminutes==payloadminutes. Task/incarnation/hashmustmatchpayload; specifiedmemberIDmustmatchONErecomputedmember. Whole/half alwaysversion2evenone-memberroster. Unknown/missing/duplicate/conflictingmarkersneverlegacyfallback.

CRITICAL: NOliteral[docket]ANYWHEREeffectiveDESCRIPTION; rejectlegacytag+v2conflict, eveninopaqueextras. UserTitleandnotesNOTputinmanifest/description. Titlemaycontainliteral[docket]inSUMMARYwithoutaffectingoldownership. PayloadASCIIandbase64closedcharacterscannotembeditag. OptionalcompletionmarkeralsohasnoDocket. DoNOTreuselegacyNewCalendar/metadatahelperwhichaddsDocket. Oldweb/nativecanDISPLAYreadonlyimports; no invisibilityclaim. Reviewbb8bf91dstaticall-actiondenialreliesonabsenceDocketevenH/M/Pone/allremoved. Manualadditionofdocket/v1ownershipretaggingOUTSIDEthatguarantee; preservecounterexamplefixtures. NewstrictreaderslaterreserveddispatchBEFORElegacyadopt/delete, requireabsentDocket+canonicalcompletevalidatedv2.

Base64urlRFC4648rawunpadded, canonicalreencode==input(noalternateunusedbits/padding). DescriptionUTF8bytecap16_384; payloadbytecap12_000; decodedmembers<=24andactualrestricted12/8. Oversizeinvalidfailclosed, no capraise. ExactDescriptioncodecforbase3linesplusoptionalcompletionline only, no useropaqueextras. M1 functionsparse/encodeonly, no account/resourceownershipclaims.

## Canonical RRULE order/abstract component fields

Subset canonicalRRULE exactorder (matches inspectedactualEventRecurrence serializer, not usedasexpectedauthority):
`FREQ=DAILY` +optional`;UNTIL=<UTCyyyyMMddTHHmmssZ>` +`;INTERVAL=1;WKST=MO;BYMINUTE=<oneortwoascendingunpaddeddecimal>` +optional`;BYDAY=MO,TU,WE,TH,FR`.
No BYHOUR/BYSECOND/COUNT/RDATE/EXRULE/otherkeys. UntilUTC comesfromstrictoriginallogicalend, sameforallmembers. Alwaysexplicitminute, no trailingsemicolons/aliases. M0Bprototype'sUNTIL-lastsemanticcodecpassed; productioncanonicalbyteorderdeliberatelymovesUNTILfirsttoavoidknownserializer-orderrewrite. No fingerprint/journal normalization allowed; recognitionsemantic/comparisonaliasesareNOTauthority.

M1Planmemberexposes hour/memberID/anchorLocal/anchorEpochMillis/minutes/rrule/durationPT5M/endEpochMillis(anchor+300000), pluscanonicalpayload/hash/descriptionbuilder. Nativeeventadapterlaterwritesdtstart,timezoneAU,dtendnull,durationPT5M. WebadapterlatermaywriteDURATIONPT5M(noDTEND) orverifiedequivalentexact5minDTEND, but M1pureplanstaysabstract300000elapsed. Mustnotcopy legacyCalendarconstructorsmetadata. Newprofileparserlatervalidatesrawcomponentagainstwholeplan; M1parseMetadataaloneisnotownership/inventoryreadiness.

SUPPORTED-COMPLETION occurrence-membership predicate for EXDATE/witness validation (not a general provider recurrence expander): require BOTH UTC year1970..2100 and Melbourne local year1970..2100, exact UTC minute, unambiguous Melbourne roundtrip; localhour==role, localminuteinmemberminutes, weekdayifwork, at>=memberanchor, at<=untilifany; logicalwholegridcondition/global>=start (redundantmustvalidate). Rejectwrongrole/outsidebounds/unsupportedinstants. Pin negative: local1970-01-01T09:17 (UTC1969-12-31T23:17Z) may compile as a valid plan but is NOT a supported completion witness. Leave both legacy helpers unchanged. Do not use this restricted witness predicate to claim that an unsupported witness is not a recurrence or to validate an elapsed end. No providerInstances synthesis/nativecalendar expansion or scheduler. Webtiminglateruseslogicalcivilprofileonlyafterfullphysicalsetverified; existinggenericDAILYiteratornotoracle.

## M1 implementation scope / acceptance

BothworkersreadownAGENTS/existingcodebeforewriting, samefrozencontract+independentfixtures. NewPUREmodule+newtests+sharedfixturecopy/doconly. NO changeslegacycompile/NewCalendar/Inventory/Repo/Gateway/defaultUI/operationstore/Nixmodule/format-v1fixture/comparator. No apps/livebuildbychildren; parentmanagedtestsafterhandoff/freshreview. RuntimeM2integrationlaterrequiresversionedjournal/full-membercapability/actualold-readerall-actionnegativefixturesanddisabledtimingedits/readersfirst. SourceCodeM1notnewcreateenabledorv2ownershipauthority.

Expectedtests: manuallyauthoredsharedfixtures(parent), exactpayload/hash/memberID/base64/description/rrule/anchor/end; whole2hsafeallphase/minutes,workhalfminute47firstclip+Mondayanchor,endemptyomission/inclusiveanchor; strict02rejectevenfiniteend, malformedmetadata/UUID/hash/unknown/order/duplicate/noncanonicalASCII/Base64/size/conflictingDocket; descriptionTitleDocketoutsidepayload; currentlegacy185unchangedatM1becausedefaultpathsNOTtouched. Additionaldateboundleap/year2100derivedoverflowcaseexplicitnegative/UNTILomit, DSTadjacent01:59duration+5elapsedbothspring/fallwithnon2members, no foldwidening. New companionM2willobserveACTUALdefault routing/intentionalpolicychanges; doNOTfakeunchanged185currentcreateswhenwriterintegrationchangespolicy. No compiler-generatedgoldens/genericallowlistwidening. Datafixturesliteral/authoredindependentlywithstandardSHA256/Base64onlynotcandidatecompiler.

Safetygates: no sourcewritesuntilrestrictedM0Bpass+independentreview(done), then THISexactcontract/fixturesindependentreview beforeM1writers. AfterM1fulltests/parityreview, finalizeM2journal/keyprovisioning/restart/membership contract BEFOREanymutationwriter. No migration/retarget/repair/automaticretry/rollback/finishmissing. AllprivateworkOwnworktrees, parentsoleGitpublisher/sourceTTBOTHdates/freshguard, noPR/historyrewrite/liveexperiments/deployment.
