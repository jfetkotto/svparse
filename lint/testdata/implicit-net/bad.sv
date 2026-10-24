module top;
  logic a;
  leaf u(.d(a), .q(out_w)); // want
  leaf v(.d(in_w), .q());   // want
  assign y = a;             // want
endmodule
module leaf(input d, output q);
endmodule
