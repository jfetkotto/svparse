module top;
  logic a;
  leaf u(.d(a), .q(out_w)); // svparse: disable=implicit-net
  // svparse: disable=implicit-net
  leaf v(.d(in_w), .q());
  // svparse: disable-next-line=all
  assign y = a;
endmodule
module leaf(input d, output q);
endmodule
