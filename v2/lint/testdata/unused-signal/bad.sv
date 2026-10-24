module top (input clk, input d, output logic q);
  logic never;            // want
  logic only_written;     // want
  wire  dead_wire;        // want
  leaf u(.d(d), .q(dead_conn));
  logic dead_conn;         // want
  always_ff @(posedge clk) begin
    q <= d;
    only_written <= d;
  end
endmodule
module leaf(input d, output q);
  assign q = d;
endmodule
